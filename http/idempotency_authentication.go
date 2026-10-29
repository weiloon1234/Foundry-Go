package http

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

type AuthenticatedIdempotentEndpoint[P, Q, B, M, R any] struct {
	source   AuthenticatedEndpoint[P, Q, B, M, R]
	endpoint IdempotentEndpoint[P, Q, B, R]
}

func IdempotentAuthenticated[P, Q, B, M, R any](endpoint AuthenticatedEndpoint[P, Q, B, M, R], store *idempotency.Store, definition idempotency.Definition) AuthenticatedIdempotentEndpoint[P, Q, B, M, R] {
	if err := ValidateAuthenticated(endpoint); err != nil {
		return AuthenticatedIdempotentEndpoint[P, Q, B, M, R]{source: endpoint, endpoint: IdempotentEndpoint[P, Q, B, R]{err: err}}
	}
	return AuthenticatedIdempotentEndpoint[P, Q, B, M, R]{source: endpoint, endpoint: Idempotent(authenticatedEndpointAuthorized(endpoint), store, definition)}
}
func ValidateAuthenticatedIdempotent[P, Q, B, M, R any](e AuthenticatedIdempotentEndpoint[P, Q, B, M, R]) error {
	if err := ValidateAuthenticated(e.source); err != nil {
		return err
	}
	return ValidateIdempotent(e.endpoint)
}
func InspectAuthenticatedIdempotent[P, Q, B, M, R any](e AuthenticatedIdempotentEndpoint[P, Q, B, M, R]) (EndpointInfo, error) {
	if err := ValidateAuthenticatedIdempotent(e); err != nil {
		return EndpointInfo{}, err
	}
	return InspectIdempotent(e.endpoint)
}
func AuthenticatedIdempotentHeaders[P, Q, B, M, R any](e AuthenticatedIdempotentEndpoint[P, Q, B, M, R], fn func(context.Context, R) ([]ResponseHeader, error)) AuthenticatedIdempotentEndpoint[P, Q, B, M, R] {
	e.endpoint = IdempotentHeaders(e.endpoint, fn)
	return e
}
func (e AuthenticatedIdempotentEndpoint[P, Q, B, M, R]) Handle(scope func(context.Context, M, Input[P, Q, B]) (idempotency.Scope, error), handler func(context.Context, *database.Tx, M, Input[P, Q, B]) (R, error)) RouteRegistration {
	if scope == nil || handler == nil {
		return InvalidRouteRegistration(safeerror.NewFault(fault.Invalid, "authenticated idempotency requires scope and transaction callbacks"))
	}
	return PrepareAuthenticatedIdempotent(e, func(ctx context.Context, actor M, in Input[P, Q, B]) (idempotency.Scope, TransactionHandler[P, Q, B, R], error) {
		identity, err := scope(ctx, actor, in)
		return identity, func(ctx context.Context, tx *database.Tx, in Input[P, Q, B]) (R, error) {
			value, err := handler(ctx, tx, actor, in)
			if err != nil {
				return *new(R), authenticationError(err)
			}
			return value, nil
		}, err
	})
}

// PrepareAuthenticatedIdempotent preserves concrete authenticated identity for model-binding adapters.
func PrepareAuthenticatedIdempotent[P, Q, B, M, R any](e AuthenticatedIdempotentEndpoint[P, Q, B, M, R], prepare func(context.Context, M, Input[P, Q, B]) (idempotency.Scope, TransactionHandler[P, Q, B, R], error)) RouteRegistration {
	if err := ValidateAuthenticatedIdempotent(e); err != nil {
		return InvalidRouteRegistration(err)
	}
	if prepare == nil {
		return InvalidRouteRegistration(safeerror.NewFault(fault.Invalid, "authenticated idempotency preparation is missing"))
	}
	return PrepareIdempotent(e.endpoint, func(ctx context.Context, in Input[P, Q, B]) (idempotency.Scope, TransactionHandler[P, Q, B, R], error) {
		actor, err := e.source.binding.guard.Require(ctx)
		if err != nil {
			return idempotency.Scope{}, nil, authenticationError(err)
		}
		// Actor authorization already ran as the endpoint's request authorization
		// stage, before validation, on new requests and replays alike.
		scope, handler, err := prepare(ctx, actor, in)
		if err != nil {
			return idempotency.Scope{}, nil, authenticationError(err)
		}
		return scope, handler, nil
	})
}
