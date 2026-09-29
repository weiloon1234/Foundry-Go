package passwords

import (
	"context"
	"net/http"

	"foundry.test/consumer/limiting"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
)

// PasswordAttempts is shared across kernels using this account provider. The
// same submitted, canonical email reaches lockout and the account lookup. A
// tenant-aware application would declare a concrete tenant+email key instead.
// Client-aware limits count failures per (email, trusted client IP) with
// account-wide and per-IP ceilings, so one source cannot lock a victim out.
var PasswordAttempts = lockout.DefineLogin("password.accounts", keyspace.StringKeys[string](), lockout.DefaultLimits())

func ProtectLogin(login *Login, store *lockout.Store) (*Login, error) {
	throttle, err := PasswordAttempts.Bind(store)
	if err != nil {
		return nil, err
	}
	return login.WithLockout(throttle)
}

// ClearPasswordFailures clears the account ceiling and the failures of the
// client making this request; other clients' windows expire on their own.
func ClearPasswordFailures(ctx context.Context, throttle lockout.Throttle[string], email string) (bool, error) {
	return throttle.Reset(ctx, email)
}

type LoginInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, LoginRequest]

// VerifyRoute is a consumer transport acceptance fixture, not a complete login
// application. It verifies a password and returns no credential. Real issuance
// consumes the typed result only after Authenticate succeeds, using TokenResponse
// or BrowserSessions for transport. Network throttling reuses the existing API.
// A standalone router has no Foundry server boundary, so TrustedProxy (with no
// trusted networks) records the peer IP used by the IP quota and lockout.
func VerifyRoute(login *Login, limits *ratelimit.Store) (http.Handler, error) {
	middleware, err := limiting.LoginMiddleware(limits)
	if err != nil {
		return nil, err
	}
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "password.verify", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/verify")),
		foundryhttp.EmptyQuery(), foundryhttp.JSONBody(LoginRequestJSON()), foundryhttp.EmptyResponse(204)).WithMiddleware(foundryhttp.TrustedProxy(foundryhttp.TrustedProxyConfig{}), middleware)
	return foundryhttp.NewRouter(endpoint.Handle(func(ctx context.Context, in LoginInput) (foundryhttp.NoContent, error) {
		_, err := Authenticate(ctx, login, in.Body)
		return foundryhttp.NoContent{}, err
	}))
}
