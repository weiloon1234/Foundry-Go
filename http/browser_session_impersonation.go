package http

import (
	"context"
	"errors"
	stdhttp "net/http"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

// ImpersonationRefusalMiddlewareID identifies RefuseImpersonation.
const ImpersonationRefusalMiddlewareID MiddlewareID = "foundry.impersonation-refusal"

// shares reports whether impersonation acts within this browser guard, so the
// actor and the impersonated model share one session cookie.
func (b *BrowserSessions[M, K]) shares(impersonation *session.Impersonation[M, K, M, K]) error {
	if err := impersonation.Validate(); err != nil {
		return err
	}
	name := b.sessions.Guard().Name()
	if impersonation.Actors().Guard().Name() != name || impersonation.Targets().Guard().Name() != name {
		return fault.New(fault.Invalid, "impersonation must use this browser guard for actors and targets")
	}
	return nil
}

// Impersonate starts impersonating target and stages the impersonation session
// as this browser's cookie. The actor's own session is kept server-side, not
// revoked, so StopImpersonating can return to it. Authorize the actor before
// calling. Use it on an authenticated, CSRF-protected unsafe route.
func (b *BrowserSessions[M, K]) Impersonate(ctx context.Context, impersonation *session.Impersonation[M, K, M, K], target model.Reference[M, K], duration time.Duration) (session.Info[M, K], error) {
	state, err := b.state(ctx)
	if err != nil {
		return session.Info[M, K]{}, err
	}
	if err := b.shares(impersonation); err != nil {
		return session.Info[M, K]{}, err
	}
	var info session.Info[M, K]
	err = state.operation(ctx, func(op context.Context) (string, time.Time, error) {
		issued, err := impersonation.Start(op, target, duration)
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

// StopImpersonating ends the impersonation behind the current cookie and stages
// the original actor's session again under a fresh secret, keeping its original
// expiry (Impersonation.Resume). When the actor's session ended meanwhile, the
// impersonation still ends, the cookie is cleared and auth.Unauthenticated is
// returned.
func (b *BrowserSessions[M, K]) StopImpersonating(ctx context.Context, impersonation *session.Impersonation[M, K, M, K]) (session.Info[M, K], error) {
	state, err := b.state(ctx)
	if err != nil {
		return session.Info[M, K]{}, err
	}
	if err := b.shares(impersonation); err != nil {
		return session.Info[M, K]{}, err
	}
	var info session.Info[M, K]
	var resumeErr error
	err = state.operation(ctx, func(op context.Context) (string, time.Time, error) {
		issued, err := impersonation.Resume(op)
		if err != nil {
			if !errors.Is(err, session.ActorSessionEnded) {
				return "", time.Time{}, err
			}
			// The impersonation session is already revoked: clear its cookie.
			resumeErr = err
			cookie, err := b.adapter.cookie.header("", true)
			return cookie, time.Time{}, err
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
	return info, resumeErr
}

// RefuseImpersonation returns actor-stage middleware that rejects impersonation
// sessions with auth.ImpersonationForbidden (HTTP 403) before decoding. Install
// it with WithActorMiddleware on routes that change credentials, email, MFA
// factors or the account itself.
func (b *BrowserSessions[M, K]) RefuseImpersonation() Middleware {
	return DefineMiddleware(ImpersonationRefusalMiddlewareID, func(next stdhttp.Handler) (stdhttp.Handler, error) {
		if err := b.Validate(); err != nil {
			return nil, err
		}
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if err := b.sessions.RequireNotImpersonating(r.Context()); err != nil {
				b.authentication.writeFailure(w, r, b.sessions.Guard().Source(), err)
				return
			}
			next.ServeHTTP(w, r)
		}), nil
	})
}
