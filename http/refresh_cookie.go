package http

import (
	"context"
	"errors"
	stdhttp "net/http"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/authtransport"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// RefreshCookie names the HttpOnly cookie that carries a browser client's
// refresh token, so JavaScript never reads it. Its attributes are fixed: the
// __Host- prefix (host-only, Path=/), Secure, HttpOnly and SameSite=Strict.
// Every endpoint that sets, reads or clears it requires POST, TLS (or a
// configured TrustedProxy), no-store and its origin protection. Give each guard
// its own name when several portals share one origin. Construction performs no I/O.
type RefreshCookie struct {
	cookie Cookie[secret.String]
	csrf   csrfPolicy
	err    error
}

func DefineRefreshCookie(name CookieName, csrf CSRFConfig) RefreshCookie {
	cookie := DefineCookie(name, SecretCookie(), CookieOptions{Path: "/", Secure: true, HTTPOnly: true, SameSite: stdhttp.SameSiteStrictMode})
	policy, err := compileCSRF(csrf)
	if !strings.HasPrefix(string(name), "__Host-") {
		err = errors.Join(err, fault.New(fault.Invalid, "refresh cookie requires a __Host- name"))
	}
	return RefreshCookie{cookie: cookie, csrf: policy, err: errors.Join(err, cookie.Validate())}
}

func (c RefreshCookie) Name() CookieName { return c.cookie.Name() }
func (c RefreshCookie) Validate() error {
	if c.err != nil {
		return c.err
	}
	if c.csrf.native == nil {
		return fault.New(fault.Invalid, "refresh cookie is not defined")
	}
	return nil
}

// set formats the cookie for the refresh secret, expiring with the family's
// current refresh-idle deadline (never past its absolute deadline).
func (c RefreshCookie) set(value secret.String, maxAge time.Duration) (string, error) {
	cookie := c.cookie
	cookie.options.MaxAge = maxAge
	return cookie.header(value.Reveal(), false)
}

// clear expires exactly this name, path and flags.
func (c RefreshCookie) clear() (string, error) { return c.cookie.header("", true) }

// RefreshCookieInfo describes an endpoint's refresh-cookie use for contract
// export: whether it reads the refresh credential from the cookie, sets it on
// success, or clears it on success. An endpoint that reads it also clears it in
// a 401 response, and must set or clear it on success: the read credential is
// consumed, so leaving it in the browser would turn the next refresh into a
// replay. It never contains a credential value.
type RefreshCookieInfo struct {
	Name   CookieName `json:"name"`
	Reads  bool       `json:"reads,omitempty"`
	Sets   bool       `json:"sets,omitempty"`
	Clears bool       `json:"clears,omitempty"`
}

func (i RefreshCookieInfo) Validate() error {
	if err := i.Name.Validate(); err != nil || !strings.HasPrefix(string(i.Name), "__Host-") || !i.Reads && !i.Sets && !i.Clears || i.Sets && i.Clears || i.Reads && !i.Sets && !i.Clears {
		return fault.New(fault.Invalid, "invalid refresh cookie metadata")
	}
	return nil
}

// TokenCookieResponse delivers an issuance for a browser client. The JSON body
// carries only the access token ({"tokens": {"access_token", "expires_in",
// "token_type"}, "mfa_required"}); a renewable token's refresh secret is set in
// the refresh cookie, expiring at the family's refresh-idle deadline, and is set
// again on every rotation. It keeps every TokenResponse safeguard and adds the
// cookie's origin protection, which also prevents login CSRF. Personal and
// MFA-pending credentials set no cookie. The clock must agree with the backend.
func TokenCookieResponse[M, K any](cookie RefreshCookie, status int, source clock.Clock) Response[token.Issued[M, K]] {
	response := credentialResponse(status, authtransport.AccessTokenResponseJSON(), func() error { return errors.Join(cookie.Validate(), credentialClockValid(source)) },
		func(_ context.Context, issued token.Issued[M, K]) (authtransport.AccessTokenResponse, error) {
			payload, err := tokenPayload(issued, source)
			if err != nil {
				return authtransport.AccessTokenResponse{}, err
			}
			return authtransport.AccessTokenResponse{Tokens: authtransport.AccessTokenPair{AccessToken: payload.Tokens.AccessToken, ExpiresIn: payload.Tokens.ExpiresIn, TokenType: payload.Tokens.TokenType}, MFARequired: payload.MFARequired}, nil
		})
	response.refreshCookie, response.refreshCookieSets = &cookie, true
	response.setRefreshCookie = func(_ context.Context, issued token.Issued[M, K]) (string, error) {
		refresh, present := issued.RefreshSecret().Get()
		if !present {
			return "", nil
		}
		now, err := credentialTime(source)
		if err != nil {
			return "", err
		}
		info := issued.Info()
		deadline, present := info.RefreshExpiresAt().Get()
		if !present {
			return "", fault.New(fault.Invalid, "renewable token has no refresh deadline")
		}
		if info.ExpiresAt().UTC().Before(deadline.UTC()) {
			deadline = info.ExpiresAt()
		}
		// Whole seconds rounded down: the cookie never outlives the credential.
		maxAge := deadline.UTC().Sub(now.UTC()).Truncate(time.Second)
		if maxAge < time.Second {
			return "", fault.New(fault.Invalid, "issued refresh credential is missing or expired")
		}
		return cookie.set(refresh, maxAge)
	}
	return response
}

// RefreshTokenCookie reads the refresh credential only from the refresh cookie.
// The request takes no body and, through EmptyQuery, no query parameters. A
// missing or malformed cookie is unauthenticated. Handlers stay as for
// RefreshTokenBody: pass Body.RefreshToken.Secret() to Tokens.Refresh. Any 401
// response of the endpoint, including a revoked or replayed family, clears the
// cookie. Pair it with TokenCookieResponse so the rotated secret is set again.
func RefreshTokenCookie(cookie RefreshCookie) Body[RefreshTokenRequest] {
	return Body[RefreshTokenRequest]{kind: payloadRefreshCookie, refreshCookie: &cookie, fromCookie: func(r *stdhttp.Request) (RefreshTokenRequest, error) {
		value, err := cookie.cookie.Read(r)
		if err != nil {
			return RefreshTokenRequest{}, auth.Unauthenticated.WithCause(err)
		}
		raw, present := value.Get()
		if !present {
			return RefreshTokenRequest{}, auth.Unauthenticated
		}
		credential, err := authtransport.NewRefreshCredential(raw)
		if err != nil {
			return RefreshTokenRequest{}, auth.Unauthenticated.WithCause(err)
		}
		return RefreshTokenRequest{RefreshToken: credential}, nil
	}}
}

// ClearRefreshCookie returns response extended to clear the refresh cookie on
// success and in a 401 its handler returns, for a logout endpoint (for example
// one calling Tokens.RevokeCurrent). A 401 from the guard's authentication
// middleware runs before the endpoint and keeps the cookie, revoking nothing.
// The endpoint gains the cookie's request protections.
func ClearRefreshCookie[R any](cookie RefreshCookie, response Response[R]) Response[R] {
	if response.refreshCookie != nil {
		response.refreshCookieErr = fault.New(fault.Invalid, "response already uses a refresh cookie")
		return response
	}
	response.refreshCookie, response.refreshCookieClears = &cookie, true
	return response
}

// refreshClearingWriter adds the clearing Set-Cookie to a 401 the endpoint
// writes, so a rejected refresh cannot leave a dead credential in the browser.
// Other failures, such as 403 or 503, keep the cookie.
type refreshClearingWriter struct {
	stdhttp.ResponseWriter
	clear string
	wrote bool
}

func (w *refreshClearingWriter) WriteHeader(status int) {
	if !w.wrote {
		w.wrote = true
		if status == stdhttp.StatusUnauthorized {
			w.Header().Add("Set-Cookie", w.clear)
		}
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *refreshClearingWriter) Write(data []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(data)
}
func (w *refreshClearingWriter) Unwrap() stdhttp.ResponseWriter { return w.ResponseWriter }

// refreshCookie is the endpoint's refresh cookie, from its body or response.
func (e Endpoint[P, Q, B, R]) refreshCookie() *RefreshCookie {
	if e.body.refreshCookie != nil {
		return e.body.refreshCookie
	}
	return e.response.refreshCookie
}

// validateRefreshCookie keeps one cookie per endpoint on an unsafe, non-replayed
// request: a refresh credential is consumed once and never idempotently replayed.
// Reading the cookie also requires a response that sets (TokenCookieResponse) or
// clears (ClearRefreshCookie) it, so the refresh secret never reaches JSON.
func (e Endpoint[P, Q, B, R]) validateRefreshCookie() error {
	cookie := e.refreshCookie()
	if cookie == nil {
		return nil
	}
	if e.body.refreshCookie != nil && e.response.refreshCookie != nil && e.body.refreshCookie.Name() != e.response.refreshCookie.Name() {
		return fault.New(fault.Invalid, "endpoint uses two refresh cookies")
	}
	if e.route.Method() != POST || e.idempotency != nil {
		return fault.New(fault.Invalid, "refresh cookie endpoints require POST without idempotent replay")
	}
	return e.refreshCookieInfo().Validate()
}

func (e Endpoint[P, Q, B, R]) refreshCookieInfo() *RefreshCookieInfo {
	cookie := e.refreshCookie()
	if cookie == nil {
		return nil
	}
	return &RefreshCookieInfo{Name: cookie.Name(), Reads: e.body.kind == payloadRefreshCookie, Sets: e.response.refreshCookieSets, Clears: e.response.refreshCookieClears}
}
