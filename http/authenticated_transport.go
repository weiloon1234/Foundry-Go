package http

import "context"

// AuthenticatedTransport composes the request/response contract with its concrete
// handler subject S. Required adapters use their model; optional adapters use
// value.Optional[Model]. Model-binding adapters preserve this distinction.
// A custom implementation owns its authentication semantics; this interface alone
// is not an authentication capability.
type AuthenticatedTransport[P, Q, B, S, R any] interface {
	Validate() error
	Description() (EndpointInfo, error)
	Handle(func(context.Context, S, Input[P, Q, B]) (R, error)) RouteRegistration
}
