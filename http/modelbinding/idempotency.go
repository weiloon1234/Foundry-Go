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

// BindIdempotent attaches a typed model resolver to an idempotent endpoint.
func BindIdempotent[P, Q, B, M, R any](endpoint foundryhttp.IdempotentEndpoint[P, Q, B, R], resolver Resolver[P, M]) IdempotentEndpoint[P, Q, B, M, R] {
	return IdempotentEndpoint[P, Q, B, M, R]{transport: endpoint, resolver: resolver}
}

// AuthorizeIdempotentResource returns an endpoint with resource authorization before
// idempotent execution or replay.
func AuthorizeIdempotentResource[P, Q, B, M, R any](endpoint IdempotentEndpoint[P, Q, B, M, R], authorize func(context.Context, Input[P, Q, B, M]) error) IdempotentEndpoint[P, Q, B, M, R] {
	endpoint.authorization = &authorize
	return endpoint
}

// ValidateIdempotentEndpoint checks resource authorization, idempotency and model
// resolver declarations.
func ValidateIdempotentEndpoint[P, Q, B, M, R any](e IdempotentEndpoint[P, Q, B, M, R]) error {
	if e.authorization != nil && *e.authorization == nil {
		return safeerror.NewFault(fault.Invalid, "resource authorization callback is missing")
	}
	if err := validateEndpointDeclaration(func() error { return foundryhttp.ValidateIdempotent(e.transport) }); err != nil {
		return err
	}
	return ValidateResolver(e.resolver)
}

// DescribeIdempotentEndpoint validates the model-binding declaration and returns
// idempotency metadata.
func DescribeIdempotentEndpoint[P, Q, B, M, R any](e IdempotentEndpoint[P, Q, B, M, R]) (foundryhttp.EndpointInfo, error) {
	if err := ValidateIdempotentEndpoint(e); err != nil {
		return foundryhttp.EndpointInfo{}, err
	}
	return foundryhttp.InspectIdempotent(e.transport)
}

// IdempotentHeaders returns an endpoint with application headers captured for exact
// replay.
func IdempotentHeaders[P, Q, B, M, R any](e IdempotentEndpoint[P, Q, B, M, R], fn func(context.Context, R) ([]foundryhttp.ResponseHeader, error)) IdempotentEndpoint[P, Q, B, M, R] {
	e.transport = foundryhttp.IdempotentHeaders(e.transport, fn)
	return e
}

// Handle resolves and authorizes the model before choosing the scope and transaction
// handler for execution or replay.
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

// AuthenticatedIdempotentEndpoint combines a typed model resolver and resource
// authorization with actor-aware idempotency.
type AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R any] struct {
	resolver      Resolver[P, M]
	authorization *func(context.Context, S, Input[P, Q, B, M]) error
	transport     foundryhttp.AuthenticatedIdempotentEndpoint[P, Q, B, S, R]
}

// BindAuthenticatedIdempotent attaches a typed model resolver to an authenticated
// idempotent endpoint.
func BindAuthenticatedIdempotent[P, Q, B, S, M, R any](endpoint foundryhttp.AuthenticatedIdempotentEndpoint[P, Q, B, S, R], resolver Resolver[P, M]) AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R] {
	return AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R]{transport: endpoint, resolver: resolver}
}

// AuthorizeAuthenticatedIdempotentResource returns an endpoint with actor-aware
// resource authorization before idempotent execution or replay.
func AuthorizeAuthenticatedIdempotentResource[P, Q, B, S, M, R any](endpoint AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R], authorize func(context.Context, S, Input[P, Q, B, M]) error) AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R] {
	endpoint.authorization = &authorize
	return endpoint
}

// ValidateAuthenticatedIdempotentEndpoint checks resource authorization, authenticated
// idempotency and model resolver declarations.
func ValidateAuthenticatedIdempotentEndpoint[P, Q, B, S, M, R any](e AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R]) error {
	if e.authorization != nil && *e.authorization == nil {
		return safeerror.NewFault(fault.Invalid, "resource authorization callback is missing")
	}
	if err := validateEndpointDeclaration(func() error { return foundryhttp.ValidateAuthenticatedIdempotent(e.transport) }); err != nil {
		return err
	}
	return ValidateResolver(e.resolver)
}

// DescribeAuthenticatedIdempotentEndpoint validates the model-binding declaration and
// returns authentication and idempotency metadata.
func DescribeAuthenticatedIdempotentEndpoint[P, Q, B, S, M, R any](e AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R]) (foundryhttp.EndpointInfo, error) {
	if err := ValidateAuthenticatedIdempotentEndpoint(e); err != nil {
		return foundryhttp.EndpointInfo{}, err
	}
	return foundryhttp.InspectAuthenticatedIdempotent(e.transport)
}

// AuthenticatedIdempotentHeaders returns an endpoint with application headers captured
// for exact replay.
func AuthenticatedIdempotentHeaders[P, Q, B, S, M, R any](e AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R], fn func(context.Context, R) ([]foundryhttp.ResponseHeader, error)) AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R] {
	e.transport = foundryhttp.AuthenticatedIdempotentHeaders(e.transport, fn)
	return e
}

// Handle resolves and authorizes the model before choosing the actor-aware scope and
// transaction handler for execution or replay.
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
