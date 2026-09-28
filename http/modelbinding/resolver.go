// Package modelbinding resolves typed route keys into concrete models before
// domain handlers run. Transport DTOs and response contracts remain explicit.
package modelbinding

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Resolver owns one lookup from decoded path P to model M. It retains no
// request result between calls. Define supports custom route keys and scopes;
// ByKey reuses a generated model query's typed primary-key lookup.
type Resolver[P, M any] struct {
	lookup func(context.Context, P) (value.Optional[M], error)
	check  func() error
	err    error
}

// Define declares a custom, context-aware lookup. Return an omitted Optional
// for an absent model. Returned errors remain ordinary domain/infrastructure
// errors; they are never treated as an absent record. The callback must be safe
// for concurrent requests and return only fully hydrated models.
func Define[P, M any](lookup func(context.Context, P) (value.Optional[M], error)) Resolver[P, M] {
	return Resolver[P, M]{lookup: lookup}
}

// Validate checks the declaration without resolving a model or opening a
// transaction. Custom declaration callbacks remain owned until they return.
func (r Resolver[P, M]) Validate() error {
	if r.err != nil {
		return r.err
	}
	if r.lookup == nil {
		return fault.New(fault.Invalid, "model binding requires a resolver")
	}
	if r.check == nil {
		return nil
	}
	return callback.Isolated("validate HTTP model resolver", r.check)
}

// Resolve performs exactly one lookup, returning 404 for an omitted result.
// A failed or canceled lookup publishes no partial model. Panic and Goexit
// remain internal failures; cancellation never abandons a running callback.
// Resolving a route model does not authorize access or lock the row.
func (r Resolver[P, M]) Resolve(ctx context.Context, path P) (M, error) {
	if ctx == nil {
		return *new(M), fault.New(fault.Invalid, "model binding requires a context")
	}
	if err := ctx.Err(); err != nil {
		return *new(M), err
	}
	if err := r.Validate(); err != nil {
		return *new(M), err
	}
	if err := ctx.Err(); err != nil {
		return *new(M), err
	}
	var result value.Optional[M]
	var returned error
	owned := callback.Isolated("HTTP model resolver", func() error {
		result, returned = r.lookup(ctx, path)
		return nil
	})
	if owned != nil {
		return *new(M), foundryhttp.InternalError.WithCause(owned)
	}
	if err := ctx.Err(); err != nil {
		return *new(M), err
	}
	if returned != nil {
		return *new(M), returned
	}
	model, present := result.Get()
	if !present {
		return *new(M), foundryhttp.NotFound
	}
	return model, nil
}
