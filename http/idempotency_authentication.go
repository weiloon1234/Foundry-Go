package http

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/idempotency"
)

type AuthenticatedIdempotentEndpoint[P, Q, B, M, R any] struct {
	source   AuthenticatedEndpoint[P, Q, B, M, R]
	endpoint IdempotentEndpoint[P, Q, B, R]
}

func (e AuthenticatedEndpoint[P, Q, B, M, R]) Idempotent(store *idempotency.Store, definition idempotency.Definition) AuthenticatedIdempotentEndpoint[P, Q, B, M, R] {
	if err := e.Validate(); err != nil {
		return AuthenticatedIdempotentEndpoint[P, Q, B, M, R]{source: e, endpoint: IdempotentEndpoint[P, Q, B, R]{err: err}}
	}
	return AuthenticatedIdempotentEndpoint[P, Q, B, M, R]{source: e, endpoint: e.bound(false).Idempotent(store, definition)}
}
func (e AuthenticatedIdempotentEndpoint[P, Q, B, M, R]) Validate() error {
	if err := e.source.Validate(); err != nil {
		return err
	}
	return e.endpoint.Validate()
}
func (e AuthenticatedIdempotentEndpoint[P, Q, B, M, R]) Description() (EndpointInfo, error) {
	if err := e.Validate(); err != nil {
		return EndpointInfo{}, err
	}
	return e.endpoint.Description()
}
func (e AuthenticatedIdempotentEndpoint[P, Q, B, M, R]) WithHeaders(fn func(context.Context, R) ([]ResponseHeader, error)) AuthenticatedIdempotentEndpoint[P, Q, B, M, R] {
	e.endpoint = e.endpoint.WithHeaders(fn)
	return e
}
func (e AuthenticatedIdempotentEndpoint[P, Q, B, M, R]) Handle(scope func(context.Context, M, Input[P, Q, B]) (idempotency.Scope, error), handler func(context.Context, *database.Tx, M, Input[P, Q, B]) (R, error)) RouteRegistration {
	if scope == nil || handler == nil {
		return InvalidRouteRegistration(fault.New(fault.Invalid, "authenticated idempotency requires scope and transaction callbacks"))
	}
	return e.Prepare(func(ctx context.Context, actor M, in Input[P, Q, B]) (idempotency.Scope, TransactionHandler[P, Q, B, R], error) {
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

// Prepare preserves concrete authenticated identity for model-binding adapters.
func (e AuthenticatedIdempotentEndpoint[P, Q, B, M, R]) Prepare(prepare func(context.Context, M, Input[P, Q, B]) (idempotency.Scope, TransactionHandler[P, Q, B, R], error)) RouteRegistration {
	if err := e.Validate(); err != nil {
		return InvalidRouteRegistration(err)
	}
	if prepare == nil {
		return InvalidRouteRegistration(fault.New(fault.Invalid, "authenticated idempotency preparation is missing"))
	}
	return e.endpoint.Prepare(func(ctx context.Context, in Input[P, Q, B]) (idempotency.Scope, TransactionHandler[P, Q, B, R], error) {
		actor, err := e.source.guard.Require(ctx)
		if err != nil {
			return idempotency.Scope{}, nil, authenticationError(err)
		}
		if e.source.authorization != nil {
			if err := requestHook(ctx, "HTTP actor request authorization", func() error { return (*e.source.authorization)(ctx, actor, in) }); err != nil {
				return idempotency.Scope{}, nil, authenticationError(err)
			}
		}
		scope, handler, err := prepare(ctx, actor, in)
		if err != nil {
			return idempotency.Scope{}, nil, authenticationError(err)
		}
		return scope, handler, nil
	})
}
