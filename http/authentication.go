package http

import (
	"context"
	stdhttp "net/http"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CredentialSource snapshots one transport input into a named secret. Sources
// never inspect URLs, request bodies, models, or serialized attribution.
type CredentialSource struct {
	name      auth.CredentialName
	info      CredentialInfo
	challenge string
	validate  func() error
	read      func(*stdhttp.Request) (value.Optional[secret.String], error)
}

// BearerCredential accepts exactly one Authorization field with the Bearer
// scheme and a bounded RFC 6750 token. Repeated/malformed values are rejected;
// a missing field is absent. A query-string token is never a fallback.
func BearerCredential(name auth.CredentialName) CredentialSource {
	const header = authorizationHeader
	return CredentialSource{name: name, info: CredentialInfo{Source: name, Kind: BearerCredentialKind, Name: header}, challenge: "Bearer", read: func(r *stdhttp.Request) (value.Optional[secret.String], error) {
		values := r.Header.Values(header)
		if len(values) == 0 {
			return value.Optional[secret.String]{}, nil
		}
		if len(values) != 1 || len(values[0]) > auth.MaxCredentialBytes+7 {
			return value.Optional[secret.String]{}, auth.Unauthenticated
		}
		scheme, token, ok := strings.Cut(values[0], " ")
		token = strings.TrimLeft(token, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || !bearerToken(token) {
			return value.Optional[secret.String]{}, auth.Unauthenticated
		}
		return value.Set(secret.New(token)), nil
	}}
}
func bearerToken(token string) bool {
	if len(token) == 0 || len(token) > auth.MaxCredentialBytes {
		return false
	}
	padding := false
	for i := range len(token) {
		c := token[i]
		if c == '=' {
			if i == 0 {
				return false
			}
			padding = true
			continue
		}
		if padding || !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~+/", rune(c))) {
			return false
		}
	}
	return true
}

// CookieCredential reuses a typed secret cookie's bounded parser, duplicate
// detection and configuration. Cookie values still require strategy verification;
// parsing is not authentication. Session cookie/CSRF policy is separate.
func CookieCredential(name auth.CredentialName, cookie Cookie[secret.String]) CredentialSource {
	return CredentialSource{name: name, info: CredentialInfo{Source: name, Kind: CookieCredentialKind, Name: string(cookie.Name())}, validate: cookie.Validate, read: cookie.Read}
}

// SecretCookie is an explicit credential transport codec: values stay redacted
// in Go and are revealed only when Cookie.Set emits the actual Set-Cookie value.
func SecretCookie() CookieCodec[secret.String] { return credentialCookieCodec{} }

type credentialCookieCodec struct{}

func (credentialCookieCodec) Parse(text string) (secret.String, error) { return secret.New(text), nil }
func (credentialCookieCodec) Format(v secret.String) (string, error)   { return v.Reveal(), nil }

// Authentication composes one registry and immutable HTTP input declarations.
// Each accepted request gets a fresh scope; callbacks finish before its cleanup.
// Construction performs no I/O and does not start a session/token service.
type Authentication struct {
	browser  *browserSessionAdapter
	csrf     *csrfPolicy
	registry *auth.Registry
	sources  []CredentialSource
}

func NewAuthentication(registry *auth.Registry, sources ...CredentialSource) (*Authentication, error) {
	if err := registry.Validate(); err != nil {
		return nil, err
	}
	if len(sources) > auth.MaxCredentials {
		return nil, fault.New(fault.Invalid, "too many HTTP credential sources")
	}
	seen := make(map[auth.CredentialName]bool, len(sources))
	for _, source := range sources {
		if !identifier.Semantic(string(source.name)) || source.read == nil {
			return nil, fault.New(fault.Invalid, "invalid HTTP credential source")
		}
		if seen[source.name] {
			return nil, fault.New(fault.Duplicate, "HTTP credential source is repeated")
		}
		seen[source.name] = true
		if source.validate != nil {
			if err := source.validate(); err != nil {
				return nil, err
			}
		}
	}
	return &Authentication{registry: registry, sources: slices.Clone(sources)}, nil
}
func (a *Authentication) validate() error {
	if a == nil {
		return fault.New(fault.Invalid, "HTTP authentication requires an adapter")
	}
	return a.registry.Validate()
}
func (a *Authentication) inputs(r *stdhttp.Request) (auth.Credentials, error) {
	var items []auth.Credential
	var credentials auth.Credentials
	err := callback.Isolated("HTTP credential extraction", func() error {
		for _, source := range a.sources {
			if err := r.Context().Err(); err != nil {
				return err
			}
			input, err := source.read(r)
			if err != nil {
				return err
			}
			if credential, ok := input.Get(); ok {
				items = append(items, auth.Credential{Name: source.name, Secret: credential})
			}
		}
		var err error
		credentials, err = auth.NewCredentials(items...)
		return err
	})
	if err != nil {
		return auth.Credentials{}, fault.Wrap(fault.Internal, "HTTP credential extraction failed", err)
	}
	return credentials, nil
}
func (a *Authentication) middleware(source auth.CredentialName, resolve func(context.Context) (context.Context, error)) Middleware {
	return defineReplayMiddleware("foundry.authentication", func(next stdhttp.Handler) (stdhttp.Handler, error) {
		if err := a.validate(); err != nil {
			return nil, err
		}
		var handler stdhttp.Handler = stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			for _, input := range a.sources {
				if input.info.Kind == CookieCredentialKind {
					privateCookieResponse(w.Header())
					break
				}
			}
			if a.csrf != nil {
				csrfVary(w.Header())
				if err := a.csrf.check(r); err != nil {
					writeRoutingError(w, r, err)
					return
				}
			}
			credentials, err := a.inputs(r)
			if err != nil {
				a.writeFailure(w, r, source, err)
				return
			}
			scope, err := a.registry.NewScope(r.Context(), credentials)
			if err != nil {
				a.writeFailure(w, r, source, err)
				return
			}
			defer scope.Close()
			r = r.WithContext(scope.Context())
			ctx, err := resolve(r.Context())
			if err != nil {
				a.writeFailure(w, r, source, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
		if a.browser != nil {
			handler = a.browser.wrap(handler)
		}
		return handler, nil
	})
}

// authenticationCode runs only inside an owned error-classification callback.
// Keep the mapping shared by public login errors and guarded transport errors.
func authenticationCode(err error) (ErrorCode, bool) {
	var locked, unavailable, unauthenticated, forbidden, mfa, timeout bool
	complete := errorgraph.Walk(err, func(current error) bool {
		locked = errorgraph.Matches(current, lockout.Locked)
		if locked {
			return false
		}
		unavailable = unavailable || errorgraph.Matches(current, lockout.Unavailable)
		unauthenticated = unauthenticated || errorgraph.Matches(current, auth.Unauthenticated) || errorgraph.Matches(current, Unauthenticated) || errorgraph.Matches(current, lockout.Expired)
		forbidden = forbidden || errorgraph.Matches(current, auth.Forbidden)
		mfa = mfa || errorgraph.Matches(current, auth.MFARequired)
		timeout = timeout || errorgraph.Matches(current, context.Canceled) || errorgraph.Matches(current, context.DeadlineExceeded)
		return true
	})
	if !complete {
		return "", false
	}
	switch {
	case locked:
		return RateLimited, true
	case unavailable && timeout:
		return RequestTimeout, true
	case unavailable:
		return Unavailable, true
	case unauthenticated:
		return Unauthenticated, true
	case forbidden:
		return Forbidden, true
	case mfa:
		return MFARequired, true
	case timeout:
		return RequestTimeout, true
	default:
		return "", false
	}
}
func authenticationError(err error) error {
	result := err
	failed := callback.Isolated("HTTP authentication error classification", func() error {
		_, found, complete := errorgraph.As[classifiedError](err)
		if !complete {
			return fault.New(fault.Invalid, "HTTP authentication error classification exceeded traversal bounds")
		}
		if found {
			return nil
		}
		if code, found := authenticationCode(err); found {
			result = code.WithCause(err)
		}
		return nil
	})
	if failed != nil {
		return InternalError.WithCause(failed)
	}
	return result
}

func (a *Authentication) hasSource(name auth.CredentialName) bool {
	for _, source := range a.sources {
		if source.name == name {
			return true
		}
	}
	return false
}

// AuthenticationInfo is inspection/client-contract metadata, never credential
// material. Guard/provider names come from the registered typed declaration.
type AuthenticationInfo struct {
	Guard               auth.GuardName         `json:"guard"`
	Provider            auth.ProviderName      `json:"provider"`
	Optional            bool                   `json:"optional"`
	Credential          CredentialInfo         `json:"credential"`
	RequiredScopes      []auth.AccessScopeName `json:"required_scopes,omitempty"`
	RequiredPermissions []auth.PermissionName  `json:"required_permissions,omitempty"`
}

func (a *Authentication) writeFailure(w stdhttp.ResponseWriter, r *stdhttp.Request, source auth.CredentialName, err error) {
	mapped := authenticationError(err)
	failure, _ := errorResponse(r.Context(), mapped)
	if failure.Code == Unauthenticated {
		for _, input := range a.sources {
			if input.name == source && input.challenge != "" {
				w.Header().Set("WWW-Authenticate", input.challenge)
				break
			}
		}
	}
	_ = WriteError(w, r, mapped)
}
