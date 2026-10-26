package modelbinding

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Models carries every concrete result in a nested lookup. Parent may itself be
// Models for another level. Nothing is placed in a heterogeneous context map.
type Models[P, C any] struct {
	Parent P
	Child  C
}

// Then resolves the parent once, then supplies that model and the original path
// to a custom child lookup. No partial bundle escapes. Callbacks borrow input,
// must be concurrency-safe and retain ownership until they return after cancel.
func Then[P, A, B any](parent Resolver[P, A], child func(context.Context, P, A) (value.Optional[B], error)) Resolver[P, Models[A, B]] {
	if child == nil {
		return Resolver[P, Models[A, B]]{err: fault.New(fault.Invalid, "nested binding requires a child resolver")}
	}
	// The composed check validates the parent once at registration; request
	// lookups reuse the validated parent without revalidating every ancestor.
	return Resolver[P, Models[A, B]]{check: parent.Validate, lookup: func(ctx context.Context, path P) (value.Optional[Models[A, B]], error) {
		a, err := parent.resolve(ctx, path)
		if err != nil {
			return value.Optional[Models[A, B]]{}, err
		}
		b, err := child(ctx, path, a)
		if err != nil {
			return value.Optional[Models[A, B]]{}, err
		}
		selected, present := b.Get()
		if !present {
			return value.Optional[Models[A, B]]{}, nil
		}
		return value.Set(Models[A, B]{Parent: a, Child: selected}), nil
	}}
}
