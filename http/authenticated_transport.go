package http

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
)

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

// AuthenticatedBinding is the binding stage of an authenticated endpoint: it
// receives the concrete subject after request authorization and before
// validation and returns the handler completing the request (see Binding).
type AuthenticatedBinding[P, Q, B, S, R any] func(context.Context, S, Input[P, Q, B]) (func(context.Context, S, Input[P, Q, B]) (R, error), error)

// boundAuthenticatedTransport is the optional binding capability of an
// AuthenticatedTransport; framework adapters implement it.
type boundAuthenticatedTransport[P, Q, B, S, R any] interface {
	HandleBound(AuthenticatedBinding[P, Q, B, S, R]) RouteRegistration
}

// bindSubject adapts an authenticated binding to an endpoint binding that
// loads the subject once per stage through subject.
func bindSubject[P, Q, B, S, R any](bind AuthenticatedBinding[P, Q, B, S, R], subject func(context.Context) (S, error)) Binding[P, Q, B, R] {
	return func(ctx context.Context, in Input[P, Q, B]) (Handler[P, Q, B, R], error) {
		actor, err := subject(ctx)
		if err != nil {
			return nil, authenticationError(err)
		}
		next, err := bind(ctx, actor, in)
		if err != nil {
			return nil, authenticationError(err)
		}
		if next == nil {
			return nil, InternalError.WithCause(fault.New(fault.Internal, "authenticated binding returned no handler"))
		}
		return func(ctx context.Context, in Input[P, Q, B]) (R, error) {
			result, err := next(ctx, actor, in)
			if err != nil {
				return *new(R), authenticationError(err)
			}
			return result, nil
		}, nil
	}
}
