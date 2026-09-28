package http

import (
	"context"
	stdhttp "net/http"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

const BrowserSessionMiddlewareID MiddlewareID = "foundry.browser-session"

// BrowserSessionConfig owns browser scope and CSRF trust. Persistence lifetimes
// come from session.Config; MaxAge/Expires are computed from issued metadata.
// Clock must agree with the persistence adapter's clock. No I/O occurs at assembly.
type BrowserSessionConfig struct {
	Cookie  CookieName
	Options CookieOptions
	CSRF    CSRFConfig
	Clock   clock.Clock
}

func DefaultBrowserSessionConfig() BrowserSessionConfig {
	return BrowserSessionConfig{Cookie: "__Host-foundry_session", Options: DefaultCookieOptions(), Clock: clock.System{}}
}
func (c BrowserSessionConfig) Validate() error {
	if nilCookieValue(c.Clock) || !c.Options.HTTPOnly || c.Options.MaxAge != 0 || !c.Options.Expires.IsZero() {
		return fault.New(fault.Invalid, "browser sessions require a clock, HttpOnly and server-owned expiry")
	}
	if err := DefineCookie(c.Cookie, SecretCookie(), c.Options).Validate(); err != nil {
		return err
	}
	return c.CSRF.Validate()
}

type browserSessionAdapter struct {
	cookie Cookie[secret.String]
	csrf   csrfPolicy
	clock  clock.Clock
}

func (a *browserSessionAdapter) now() (time.Time, error) {
	now := a.clock.Now().UTC()
	instant, err := temporal.NewDateTime(now)
	if err != nil {
		return time.Time{}, err
	}
	if instant.IsZero() {
		return time.Time{}, fault.New(fault.Invalid, "browser session clock returned zero time")
	}
	return now, nil
}

// BrowserSessions connects a typed persistent guard to HTTP cookies. Its
// Authentication automatically establishes CSRF and cookie ownership. Public
// login routes use Middleware. Operations require an ordinary typed JSON/empty
// endpoint, one credential change per request, and an unsafe HTTP method.
type BrowserSessions[M model.Identifiable, K any] struct {
	sessions       *session.Sessions[M, K]
	adapter        *browserSessionAdapter
	authentication *Authentication
}

func NewBrowserSessions[M model.Identifiable, K any](registry *auth.Registry, sessions *session.Sessions[M, K], config BrowserSessionConfig) (*BrowserSessions[M, K], error) {
	if err := sessions.Validate(); err != nil {
		return nil, err
	}
	if err := sessions.Guard().ValidateIn(registry); err != nil {
		return nil, err
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	policy, err := compileCSRF(config.CSRF)
	if err != nil {
		return nil, err
	}
	adapter := &browserSessionAdapter{cookie: DefineCookie(config.Cookie, SecretCookie(), config.Options), csrf: policy, clock: config.Clock}
	source := CredentialSource{name: sessions.Guard().Source(), info: CredentialInfo{Source: sessions.Guard().Source(), Kind: CookieCredentialKind, Name: string(config.Cookie), OriginProtection: true}, read: func(r *stdhttp.Request) (value.Optional[secret.String], error) {
		state := browserState(r.Context())
		if state == nil || state.owner != adapter {
			return value.Optional[secret.String]{}, fault.New(fault.Missing, "browser session request is not bound")
		}
		state.mu.Lock()
		defer state.mu.Unlock()
		if state.sealed {
			return value.Optional[secret.String]{}, fault.New(fault.Missing, "browser session request is closed")
		}
		return state.credential, nil
	}}
	transport, err := NewAuthentication(registry, source)
	if err != nil {
		return nil, err
	}
	transport.browser = adapter
	return &BrowserSessions[M, K]{sessions: sessions, adapter: adapter, authentication: transport}, nil
}
func (b *BrowserSessions[M, K]) Validate() error {
	if b == nil || b.adapter == nil || b.authentication == nil {
		return fault.New(fault.Invalid, "browser sessions are not configured")
	}
	return b.sessions.Validate()
}
func (b *BrowserSessions[M, K]) Authentication() *Authentication {
	if b == nil {
		return nil
	}
	return b.authentication
}
func (b *BrowserSessions[M, K]) Guard() auth.Guard[M] {
	if b == nil {
		return auth.Guard[M]{}
	}
	return b.sessions.Guard()
}
func (b *BrowserSessions[M, K]) Middleware() Middleware {
	return defineReplayMiddleware(BrowserSessionMiddlewareID, func(next stdhttp.Handler) (stdhttp.Handler, error) {
		if err := b.Validate(); err != nil {
			return nil, err
		}
		return b.adapter.wrap(next), nil
	})
}
func (a *browserSessionAdapter) wrap(next stdhttp.Handler) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		privateCookieResponse(w.Header())
		if existing := browserState(r.Context()); existing != nil {
			if existing.owner != a {
				writeRoutingError(w, r, InternalError)
				return
			}
			existing.mu.Lock()
			closed := existing.sealed
			existing.mu.Unlock()
			if closed {
				writeRoutingError(w, r, InternalError)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		csrfVary(w.Header())
		credential, err := a.readCredential(r)
		if err != nil {
			writeRoutingError(w, r, err)
			return
		}
		owner, cancel := context.WithCancel(r.Context())
		state := &browserSessionState{owner: a, context: owner, cancel: cancel, credential: credential, method: r.Method}
		defer state.close()
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), browserSessionKey{}, state)))
	})
}

func (a *browserSessionAdapter) readCredential(r *stdhttp.Request) (value.Optional[secret.String], error) {
	if err := a.csrf.check(r); err != nil {
		return value.Optional[secret.String]{}, err
	}
	if a.cookie.options.Secure && !IsSecure(r) {
		return value.Optional[secret.String]{}, BadRequest
	}
	credential, err := a.cookie.Read(r)
	if err != nil {
		return value.Optional[secret.String]{}, err
	}
	if token, present := credential.Get(); present {
		if _, err := session.HashSecret(token); err != nil {
			return value.Optional[secret.String]{}, authenticationError(err)
		}
	}
	return credential, nil
}
func (b *BrowserSessions[M, K]) state(ctx context.Context) (*browserSessionState, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	state := browserState(ctx)
	if state == nil || state.owner != b.adapter {
		return nil, fault.New(fault.Missing, "browser session request is not bound to this adapter")
	}
	return state, nil
}
func (b *BrowserSessions[M, K]) issueCookie(ctx context.Context, issued session.Issued[M, K]) (string, time.Time, error) {
	info := issued.Info()
	deadline := info.IdleExpiresAt().UTC()
	if info.ExpiresAt().UTC().Before(deadline) {
		deadline = info.ExpiresAt().UTC()
	}
	now, err := b.adapter.now()
	if err != nil {
		return "", time.Time{}, err
	}
	if !now.Before(deadline) {
		return "", time.Time{}, auth.Unauthenticated
	}
	cookie := b.adapter.cookie
	if info.Remembered() {
		cookie.options.Expires = info.ExpiresAt().UTC()
		// MaxAge has second precision; expiry remains authoritative on the server.
		cookie.options.MaxAge = time.Duration((cookie.options.Expires.Sub(now)+time.Second-1)/time.Second) * time.Second
	}
	text, err := cookie.format(ctx, issued.Secret())
	if err != nil {
		return "", time.Time{}, err
	}
	header, err := cookie.header(text, false)
	return header, deadline, err
}

// Login issues a fresh credential from a trusted verified proof, then revokes the
// supplied previous cookie before staging its replacement. Errors publish neither
// secret nor cookie. Issuance counts against the active-session limit; a lost/error
// response can leave an unexposed session until expiry or explicit revocation.
func (b *BrowserSessions[M, K]) Login(ctx context.Context, proof auth.Proof[M, K], options session.IssueOptions) (session.Info[M, K], error) {
	state, err := b.state(ctx)
	if err != nil {
		return session.Info[M, K]{}, err
	}
	var info session.Info[M, K]
	err = state.operation(ctx, func(op context.Context) (string, time.Time, error) {
		issued, err := b.sessions.Issue(op, proof, options)
		if err != nil {
			return "", time.Time{}, err
		}
		if old, present := state.credential.Get(); present {
			if _, err := b.sessions.Revoke(op, old); err != nil {
				return "", time.Time{}, err
			}
		}
		cookie, deadline, err := b.issueCookie(op, issued)
		if err != nil {
			return "", time.Time{}, err
		}
		info = issued.Info()
		return cookie, deadline, nil
	})
	if err != nil {
		return session.Info[M, K]{}, err
	}
	return info, nil
}

// CompleteMFA consumes the bound pending cookie and stages a fresh full session.
// Use Middleware on this public challenge endpoint: an ordinary guarded endpoint
// rejects pending credentials. The existing CSRF, one-operation and response
// ownership checks apply. No cookie is published if completion/response fails.
// Completion may already have committed if later response preparation fails;
// the caller must restart login instead of replaying the consumed challenge.
func (b *BrowserSessions[M, K]) CompleteMFA(ctx context.Context, factor auth.SecondFactor[M, K], options session.IssueOptions) (session.Info[M, K], error) {
	state, err := b.state(ctx)
	if err != nil {
		return session.Info[M, K]{}, err
	}
	var info session.Info[M, K]
	err = state.operation(ctx, func(op context.Context) (string, time.Time, error) {
		pending, present := state.credential.Get()
		if !present {
			return "", time.Time{}, auth.Unauthenticated
		}
		issued, err := b.sessions.CompleteMFA(op, pending, factor, options)
		if err != nil {
			return "", time.Time{}, err
		}
		cookie, deadline, err := b.issueCookie(op, issued)
		if err != nil {
			return "", time.Time{}, err
		}
		info = issued.Info()
		return cookie, deadline, nil
	})
	if err != nil {
		return session.Info[M, K]{}, err
	}
	return info, nil
}

func (b *BrowserSessions[M, K]) Rotate(ctx context.Context) (session.Info[M, K], error) {
	state, err := b.state(ctx)
	if err != nil {
		return session.Info[M, K]{}, err
	}
	var info session.Info[M, K]
	err = state.operation(ctx, func(op context.Context) (string, time.Time, error) {
		old, present := state.credential.Get()
		if !present {
			return "", time.Time{}, auth.Unauthenticated
		}
		issued, err := b.sessions.Rotate(op, old)
		if err != nil {
			return "", time.Time{}, err
		}
		cookie, deadline, err := b.issueCookie(op, issued)
		if err != nil {
			return "", time.Time{}, err
		}
		info = issued.Info()
		return cookie, deadline, nil
	})
	if err != nil {
		return session.Info[M, K]{}, err
	}
	return info, nil
}
func (b *BrowserSessions[M, K]) Logout(ctx context.Context) error {
	state, err := b.state(ctx)
	if err != nil {
		return err
	}
	return state.operation(ctx, func(op context.Context) (string, time.Time, error) {
		if old, present := state.credential.Get(); present {
			if _, err := b.sessions.Revoke(op, old); err != nil {
				return "", time.Time{}, err
			}
		}
		cookie, err := b.adapter.cookie.header("", true)
		return cookie, time.Time{}, err
	})
}
