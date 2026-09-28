package http

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	stdhttp "net/http"
)

// PreservesIdempotentResponses explicitly attests a custom wrapper's contract:
// it may reject before execution or change transport/security headers, but must
// preserve downstream application status/body/declared replay headers, must not
// issue cookies/credentials, and must not execute a request more than once.
// Compression of the stored application representation is permitted. Arbitrary
// wrappers outside Foundry's assembly remain their caller's responsibility.
func (m Middleware) PreservesIdempotentResponses() Middleware { m.idempotent = true; return m }
func defineReplayMiddleware(id MiddlewareID, construct func(stdhttp.Handler) (stdhttp.Handler, error)) Middleware {
	return DefineMiddleware(id, construct).PreservesIdempotentResponses()
}
func validateIdempotentMiddlewares(items []Middleware) error {
	for _, item := range items {
		if !item.idempotent {
			return fault.New(fault.Invalid, "middleware has no idempotent-response contract: "+string(item.id))
		}
	}
	return nil
}

type idempotentHTTPHandler struct{ stdhttp.Handler }

func (idempotentHTTPHandler) requiresIdempotentMiddleware() bool { return true }
func (r *Router) requiresIdempotentMiddleware() bool {
	if r == nil {
		return false
	}
	for _, snapshot := range r.endpoints {
		if snapshot().Idempotency != nil {
			return true
		}
	}
	return false
}
func requiresIdempotentMiddleware(handler stdhttp.Handler) bool {
	aware, ok := handler.(interface{ requiresIdempotentMiddleware() bool })
	return ok && aware.requiresIdempotentMiddleware()
}
