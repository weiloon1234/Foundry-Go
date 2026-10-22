package modelbinding

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/idempotency"
)

// IdempotentEndpoint resolves and authorizes the resource on every request, then
// supplies the same concrete model and request to a transaction-bound callback.
type IdempotentEndpoint[P, Q, B, M, R any] struct {
	source    Endpoint[P, Q, B, M, R]
	transport foundryhttp.IdempotentEndpoint[P, Q, B, R]
	err       error
}

func Idempotent[P, Q, B, M, R any](endpoint Endpoint[P, Q, B, M, R], store *idempotency.Store, definition idempotency.Definition) IdempotentEndpoint[P, Q, B, M, R] {
	adapted := IdempotentEndpoint[P, Q, B, M, R]{source: endpoint}
	switch transport := endpoint.transport.(type) {
	case foundryhttp.Endpoint[P, Q, B, R]:
		adapted.transport = foundryhttp.Idempotent(transport, store, definition)
	case foundryhttp.SignedEndpoint[P, Q, B, R]:
		adapted.transport = foundryhttp.IdempotentSigned(transport, store, definition)
	default:
		adapted.err = fault.New(fault.Invalid, "model-bound transport does not support idempotent operations")
		return adapted
	}
	return adapted
}
func (e IdempotentEndpoint[P, Q, B, M, R]) Validate() error {
	for _, err := range []error{e.err, e.source.Validate(), e.transport.Validate()} {
		if err != nil {
			return err
		}
	}
	return nil
}
func (e IdempotentEndpoint[P, Q, B, M, R]) Description() (foundryhttp.EndpointInfo, error) {
	if err := e.Validate(); err != nil {
		return foundryhttp.EndpointInfo{}, err
	}
	return e.transport.Description()
}
func (e IdempotentEndpoint[P, Q, B, M, R]) WithHeaders(fn func(context.Context, R) ([]foundryhttp.ResponseHeader, error)) IdempotentEndpoint[P, Q, B, M, R] {
	e.transport = e.transport.WithHeaders(fn)
	return e
}
func (e IdempotentEndpoint[P, Q, B, M, R]) Handle(scope func(context.Context, Input[P, Q, B, M]) (idempotency.Scope, error), handler func(context.Context, *database.Tx, Input[P, Q, B, M]) (R, error)) foundryhttp.RouteRegistration {
	if err := e.Validate(); err != nil {
		return foundryhttp.InvalidRouteRegistration(err)
	}
	if scope == nil || handler == nil {
		return foundryhttp.InvalidRouteRegistration(fault.New(fault.Invalid, "model-bound idempotency needs scope and transaction callbacks"))
	}
	return e.transport.Prepare(func(ctx context.Context, in foundryhttp.Input[P, Q, B]) (idempotency.Scope, foundryhttp.TransactionHandler[P, Q, B, R], error) {
		bound, err := resolveInput(ctx, e.source.resolver, in)
		if err != nil {
			return idempotency.Scope{}, nil, err
		}
		if e.source.authorization != nil {
			if err := authorizeResource(ctx, func() error { return (*e.source.authorization)(ctx, bound) }); err != nil {
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
	source    AuthenticatedEndpoint[P, Q, B, S, M, R]
	transport foundryhttp.AuthenticatedIdempotentEndpoint[P, Q, B, S, R]
	err       error
}

func IdempotentAuthenticated[P, Q, B, S, M, R any](endpoint AuthenticatedEndpoint[P, Q, B, S, M, R], store *idempotency.Store, definition idempotency.Definition) AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R] {
	adapted := AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R]{source: endpoint}
	switch transport := endpoint.transport.(type) {
	case foundryhttp.AuthenticatedEndpoint[P, Q, B, S, R]:
		adapted.transport = foundryhttp.IdempotentAuthenticated(transport, store, definition)
	default:
		adapted.err = fault.New(fault.Invalid, "authenticated transport does not support required idempotent identity")
		return adapted
	}
	return adapted
}
func (e AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R]) Validate() error {
	for _, err := range []error{e.err, e.source.Validate(), e.transport.Validate()} {
		if err != nil {
			return err
		}
	}
	return nil
}
func (e AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R]) Description() (foundryhttp.EndpointInfo, error) {
	if err := e.Validate(); err != nil {
		return foundryhttp.EndpointInfo{}, err
	}
	return e.transport.Description()
}
func (e AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R]) WithHeaders(fn func(context.Context, R) ([]foundryhttp.ResponseHeader, error)) AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R] {
	e.transport = e.transport.WithHeaders(fn)
	return e
}
func (e AuthenticatedIdempotentEndpoint[P, Q, B, S, M, R]) Handle(scope func(context.Context, S, Input[P, Q, B, M]) (idempotency.Scope, error), handler func(context.Context, *database.Tx, S, Input[P, Q, B, M]) (R, error)) foundryhttp.RouteRegistration {
	if err := e.Validate(); err != nil {
		return foundryhttp.InvalidRouteRegistration(err)
	}
	if scope == nil || handler == nil {
		return foundryhttp.InvalidRouteRegistration(fault.New(fault.Invalid, "authenticated model idempotency needs scope and transaction callbacks"))
	}
	return e.transport.Prepare(func(ctx context.Context, actor S, in foundryhttp.Input[P, Q, B]) (idempotency.Scope, foundryhttp.TransactionHandler[P, Q, B, R], error) {
		bound, err := resolveInput(ctx, e.source.resolver, in)
		if err != nil {
			return idempotency.Scope{}, nil, err
		}
		if e.source.authorization != nil {
			if err := authorizeResource(ctx, func() error { return (*e.source.authorization)(ctx, actor, bound) }); err != nil {
				return idempotency.Scope{}, nil, err
			}
		}
		identity, err := scope(ctx, actor, bound)
		return identity, func(ctx context.Context, tx *database.Tx, _ foundryhttp.Input[P, Q, B]) (R, error) {
			return handler(ctx, tx, actor, bound)
		}, err
	})
}
