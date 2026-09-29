package modelbinding

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// AuthenticatedEndpoint retains two independent model types: the transport's
// subject S and the bound resource M. For an optional transport S is Optional[A].
// A resource lookup is not authorization; apply a concrete resource policy too.
type AuthenticatedEndpoint[P, Q, B, S, M, R any] struct {
	transport     foundryhttp.AuthenticatedTransport[P, Q, B, S, R]
	resolver      Resolver[P, M]
	authorization *func(context.Context, S, Input[P, Q, B, M]) error
}

func BindAuthenticated[P, Q, B, S, M, R any](transport foundryhttp.AuthenticatedTransport[P, Q, B, S, R], resolver Resolver[P, M]) AuthenticatedEndpoint[P, Q, B, S, M, R] {
	return AuthenticatedEndpoint[P, Q, B, S, M, R]{transport: transport, resolver: resolver}
}
func (e AuthenticatedEndpoint[P, Q, B, S, M, R]) Validate() error {
	if e.authorization != nil && *e.authorization == nil {
		return fault.New(fault.Invalid, "resource authorization callback is missing")
	}
	if err := validateTransport(e.transport); err != nil {
		return err
	}
	return e.resolver.Validate()
}
func (e AuthenticatedEndpoint[P, Q, B, S, M, R]) Description() (foundryhttp.EndpointInfo, error) {
	if err := e.Validate(); err != nil {
		return foundryhttp.EndpointInfo{}, err
	}
	return e.transport.Description()
}
func (e AuthenticatedEndpoint[P, Q, B, S, M, R]) Handle(handler func(context.Context, S, Input[P, Q, B, M]) (R, error)) foundryhttp.RouteRegistration {
	if err := e.Validate(); err != nil {
		return foundryhttp.InvalidRouteRegistration(err)
	}
	if handler == nil {
		return foundryhttp.InvalidRouteRegistration(fault.New(fault.Invalid, "authenticated model binding requires a handler"))
	}
	resolve := func(ctx context.Context, subject S, in foundryhttp.Input[P, Q, B]) (Input[P, Q, B, M], error) {
		bound, err := resolveInput(ctx, e.resolver, in)
		if err != nil {
			return Input[P, Q, B, M]{}, err
		}
		if e.authorization != nil {
			if err := authorizeResource(ctx, func() error { return (*e.authorization)(ctx, subject, bound) }); err != nil {
				return Input[P, Q, B, M]{}, err
			}
		}
		return bound, nil
	}
	// Framework transports resolve at their binding stage, before validation;
	// see Bind.
	if staged, ok := e.transport.(boundAuthenticatedTransport[P, Q, B, S, R]); ok {
		return staged.HandleBound(func(ctx context.Context, subject S, in foundryhttp.Input[P, Q, B]) (func(context.Context, S, foundryhttp.Input[P, Q, B]) (R, error), error) {
			bound, err := resolve(ctx, subject, in)
			if err != nil {
				return nil, err
			}
			return func(ctx context.Context, subject S, in foundryhttp.Input[P, Q, B]) (R, error) {
				return handler(ctx, subject, Input[P, Q, B, M]{Request: in, Model: bound.Model})
			}, nil
		})
	}
	return e.transport.Handle(func(ctx context.Context, subject S, in foundryhttp.Input[P, Q, B]) (R, error) {
		bound, err := resolve(ctx, subject, in)
		if err != nil {
			return *new(R), err
		}
		return handler(ctx, subject, bound)
	})
}

// boundAuthenticatedTransport is the optional binding capability of framework
// authenticated endpoints.
type boundAuthenticatedTransport[P, Q, B, S, R any] interface {
	HandleBound(foundryhttp.AuthenticatedBinding[P, Q, B, S, R]) foundryhttp.RouteRegistration
}
