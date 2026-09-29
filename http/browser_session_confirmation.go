package http

import (
	stdhttp "net/http"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// PasswordConfirmationMiddlewareID identifies RequirePasswordConfirmation.
const PasswordConfirmationMiddlewareID MiddlewareID = "foundry.password-confirmation"

// RequirePasswordConfirmation returns actor-stage middleware for sensitive routes
// authenticated by this browser guard. It rejects the request with
// auth.ConfirmationRequired (HTTP 403) unless the current session re-entered its
// password within the window, checked against the session store's clock. Install
// it with WithActorMiddleware. A confirmation route re-verifies the password
// (auth.PasswordLogin.Confirm) and then calls session.Sessions.ConfirmCurrent.
func (b *BrowserSessions[M, K]) RequirePasswordConfirmation(within time.Duration) Middleware {
	return DefineMiddleware(PasswordConfirmationMiddlewareID, func(next stdhttp.Handler) (stdhttp.Handler, error) {
		if err := b.Validate(); err != nil {
			return nil, err
		}
		if within <= 0 {
			return nil, fault.New(fault.Invalid, "password confirmation window must be positive")
		}
		if next == nil {
			return nil, fault.New(fault.Invalid, "password confirmation requires a next handler")
		}
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if err := b.sessions.RequireConfirmed(r.Context(), within); err != nil {
				b.authentication.writeFailure(w, r, b.sessions.Guard().Source(), err)
				return
			}
			next.ServeHTTP(w, r)
		}), nil
	})
}
