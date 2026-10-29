package modelbinding

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// IdempotentEndpoint resolves and authorizes the resource on every request, then
// supplies the same concrete model and request to a transaction-bound callback.
type IdempotentEndpoint[P, Q, B, M, R any] struct {
	resolver      Resolver[P, M]
	authorization *func(context.Context, Input[P, Q, B, M]) error
	transport     foundryhttp.IdempotentEndpoint[P, Q, B, R]
}

func BindIdempotent[P, Q, B, M, R any](endpoint foundryhttp.IdempotentEndpoint[P, Q, B, R], resolver Resolver[P, M]) IdempotentEndpoint[P, Q, B, M, R] {
	return IdempotentEndpoint[P, Q, B, M, R]{transport: endpoint, resolver: resolver}
}
func AuthorizeIdempotentResource[P, Q, B, M, R any](endpoint IdempotentEndpoint[P, Q, B, M, R], authorize func(context.Context, Input[P, Q, B, M]) error) IdempotentEndpoint[P, Q, B, M, R] {
	endpoint.authorization = &authorize
	return endpoint
}
func ValidateIdempotentEndpoint[P, Q, B, M, R any](e IdempotentEndpoint[P, Q, B, M, R]) error {
	if e.authorization != nil && *e.authorization == nil {
		return safeerror.NewFault(fault.Invalid, "resource authorization callback is missing")
	}
	if err := validateEndpointDeclaration(func() error { return foundryhttp.ValidateIdempotent(e.transport) }); err != nil {
		return err
	}
	return ValidateResolver(e.resolver)
}
func DescribeIdempotentEndpoint[P, Q, B, M, R any](e IdempotentEndpoint[P, Q, B, M, R]) (foundryhttp.EndpointInfo, error) {
	if err := ValidateIdempotentEndpoint(e); err != nil {
		return foundryhttp.EndpointInfo{}, err
	}
	return foundryhttp.InspectIdempotent(e.transport)
}
func IdempotentHeaders[P, Q, B, M, R any](e IdempotentEndpoint[P, Q, B, M, R], fn func(context.Context, R) ([]foundryhttp.ResponseHeader, error)) IdempotentEndpoint[P, Q, B, M, R] {
	e.transport = foundryhttp.IdempotentHeaders(e.transport, fn)
	return e
}
func (e IdempotentEndpoint[P, Q, B, M, R]) Handle(scope func(context.Context, Input[P, Q, B, M]) (idempotency.Scope, error), handler func(context.Context, *database.Tx, Input[P, Q, B, M]) (R, error)) foundryhttp.RouteRegistration {
	if err := ValidateIdempotentEndpoint(e); err != nil {
		return foundryhttp.InvalidRouteRegistration(err)
	}
	if scope == nil || handler == nil {
		return foundryhttp.InvalidRouteRegistration(safeerror.NewFault(fault.Invalid, "model-bound idempotency needs scope and transaction callbacks"))
	}
	return foundryhttp.PrepareIdempotent(e.transport, func(ctx context.Context, in foundryhttp.Input[P, Q, B]) (idempotency.Scope, foundryhttp.TransactionHandler[P, Q, B, R], error) {
		bound, err := resolveInput(ctx, e.resolver, in)
		if err != nil {
			return idempotency.Scope{}, nil, err
		}
		if e.authorization != nil {
			if err := authorizeResource(ctx, func() error { return (*e.authorization)(ctx, bound) }); err != nil {
				return idempotency.Scope{}, nil, err
			}
		}
		identity, err := scope(ctx, bound)
		return identity, func(ctx context.Context, tx *database.Tx, _ foundryhttp.Input[P, Q, B]) (R, error) {
			return handler(ctx, tx, bound)
		}, err
	})
}

type AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R any] struct {
	resolver      Resolver[P, M]
	authorization *func(context.Context, S, Input[P, Q, B, M]) error
	transport     foundryhttp.AuthenticatedIdempotentEndpoint[P, Q, B, S, R]
}

func BindAuthenticatedIdempotent[P, Q, B, S, M, R any](endpoint foundryhttp.AuthenticatedIdempotentEndpoint[P, Q, B, S, R], resolver Resolver[P, M]) AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R] {
	return AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R]{transport: endpoint, resolver: resolver}
}
func AuthorizeAuthenticatedIdempotentResource[P, Q, B, S, M, R any](endpoint AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R], authorize func(context.Context, S, Input[P, Q, B, M]) error) AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R] {
	endpoint.authorization = &authorize
	return endpoint
}
func ValidateAuthenticatedIdempotentEndpoint[P, Q, B, S, M, R any](e AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R]) error {
	if e.authorization != nil && *e.authorization == nil {
		return safeerror.NewFault(fault.Invalid, "resource authorization callback is missing")
	}
	if err := validateEndpointDeclaration(func() error { return foundryhttp.ValidateAuthenticatedIdempotent(e.transport) }); err != nil {
		return err
	}
	return ValidateResolver(e.resolver)
}
func DescribeAuthenticatedIdempotentEndpoint[P, Q, B, S, M, R any](e AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R]) (foundryhttp.EndpointInfo, error) {
	if err := ValidateAuthenticatedIdempotentEndpoint(e); err != nil {
		return foundryhttp.EndpointInfo{}, err
	}
	return foundryhttp.InspectAuthenticatedIdempotent(e.transport)
}
func AuthenticatedIdempotentHeaders[P, Q, B, S, M, R any](e AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R], fn func(context.Context, R) ([]foundryhttp.ResponseHeader, error)) AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R] {
	e.transport = foundryhttp.AuthenticatedIdempotentHeaders(e.transport, fn)
	return e
}
func (e AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R]) Handle(scope func(context.Context, S, Input[P, Q, B, M]) (idempotency.Scope, error), handler func(context.Context, *database.Tx, S, Input[P, Q, B, M]) (R, error)) foundryhttp.RouteRegistration {
	if err := ValidateAuthenticatedIdempotentEndpoint(e); err != nil {
		return foundryhttp.InvalidRouteRegistration(err)
	}
	if scope == nil || handler == nil {
		return foundryhttp.InvalidRouteRegistration(safeerror.NewFault(fault.Invalid, "authenticated model idempotency needs scope and transaction callbacks"))
	}
	return foundryhttp.PrepareAuthenticatedIdempotent(e.transport, func(ctx context.Context, actor S, in foundryhttp.Input[P, Q, B]) (idempotency.Scope, foundryhttp.TransactionHandler[P, Q, B, R], error) {
		bound, err := resolveInput(ctx, e.resolver, in)
		if err != nil {
			return idempotency.Scope{}, nil, err
		}
		if e.authorization != nil {
			if err := authorizeResource(ctx, func() error { return (*e.authorization)(ctx, actor, bound) }); err != nil {
				return idempotency.Scope{}, nil, err
			}
		}
		identity, err := scope(ctx, actor, bound)
		return identity, func(ctx context.Context, tx *database.Tx, _ foundryhttp.Input[P, Q, B]) (R, error) {
			return handler(ctx, tx, actor, bound)
		}, err
	})
}
