package modelbinding

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// Input preserves the original typed transport request alongside its loaded
// model. The model is available to domain code, never added to the wire schema.
type Input[P, Q, B, M any] struct {
	Request foundryhttp.Input[P, Q, B]
	Model   M
}

// Transport is the endpoint capability used by the binding adapter. Ordinary
// Endpoint and SignedEndpoint both satisfy it, preserving signature checks and
// all existing middleware, decoding, validation, errors and response contracts.
type Transport[P, Q, B, R any] interface {
	Validate() error
	Description() (foundryhttp.EndpointInfo, error)
	Handle(foundryhttp.Handler[P, Q, B, R]) foundryhttp.RouteRegistration
}

// Endpoint binds one concrete model to a transport declaration. Configure the
// original endpoint before Bind; use it for typed URL generation as usual.
type Endpoint[P, Q, B, M, R any] struct {
	transport     Transport[P, Q, B, R]
	resolver      Resolver[P, M]
	authorization *func(context.Context, Input[P, Q, B, M]) error
}

// Bind composes model resolution after request decoding and validation, before
// the domain handler. No database I/O occurs during construction/registration.
func Bind[P, Q, B, M, R any](transport Transport[P, Q, B, R], resolver Resolver[P, M]) Endpoint[P, Q, B, M, R] {
	return Endpoint[P, Q, B, M, R]{transport: transport, resolver: resolver}
}

func (e Endpoint[P, Q, B, M, R]) Validate() error {
	if e.authorization != nil && *e.authorization == nil {
		return fault.New(fault.Invalid, "resource authorization callback is missing")
	}
	if err := validateTransport(e.transport); err != nil {
		return err
	}
	return e.resolver.Validate()
}

// Description retains transport metadata without treating the stored model as
// a public request or response DTO. Model lookup does not change wire types.
func (e Endpoint[P, Q, B, M, R]) Description() (foundryhttp.EndpointInfo, error) {
	if err := e.Validate(); err != nil {
		return foundryhttp.EndpointInfo{}, err
	}
	return e.transport.Description()
}

// Handle requires the declared model, path/query/body and response types.
// The resolver runs once per accepted request. It does not start a transaction
// or cache a model across requests; the handler owns business transactions.
func (e Endpoint[P, Q, B, M, R]) Handle(handler func(context.Context, Input[P, Q, B, M]) (R, error)) foundryhttp.RouteRegistration {
	if err := e.Validate(); err != nil {
		return foundryhttp.InvalidRouteRegistration(err)
	}
	if handler == nil {
		return foundryhttp.InvalidRouteRegistration(fault.New(fault.Invalid, "model binding requires a handler"))
	}
	return e.transport.Handle(func(ctx context.Context, in foundryhttp.Input[P, Q, B]) (R, error) {
		bound, err := resolveInput(ctx, e.resolver, in)
		if err != nil {
			return *new(R), err
		}
		if e.authorization != nil {
			if err := authorizeResource(ctx, func() error { return (*e.authorization)(ctx, bound) }); err != nil {
				return *new(R), err
			}
		}
		return handler(ctx, bound)
	})
}

func validateTransport(transport interface{ Validate() error }) error {
	if nilValue(transport) {
		return fault.New(fault.Invalid, "model binding requires an endpoint")
	}
	return callback.Isolated("validate model-bound endpoint", transport.Validate)
}
func resolveInput[P, Q, B, M any](ctx context.Context, resolver Resolver[P, M], in foundryhttp.Input[P, Q, B]) (Input[P, Q, B, M], error) {
	model, err := resolver.Resolve(ctx, in.Path)
	if err != nil {
		return Input[P, Q, B, M]{}, err
	}
	if err := ctx.Err(); err != nil {
		return Input[P, Q, B, M]{}, err
	}
	return Input[P, Q, B, M]{Request: in, Model: model}, nil
}
