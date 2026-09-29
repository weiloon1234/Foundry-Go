package modelbinding

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
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

// Through adds one uniquely keyed child through the loaded parent's declared
// direct relationship. Missing/out-of-scope children are 404. A malformed path
// or failed request authorization is rejected by the transport before lookup.
func Through[P, A, B any, K comparable](executor database.Executor, parent Resolver[P, A], relation query.ScopedRelation[A, B], field query.KeyField[B, K], key func(P) K) Resolver[P, Models[A, B]] {
	return ThroughSelected(executor, parent, func(a A) A { return a }, relation, field, key)
}

// ThroughSelected adds a deeper child while preserving the complete earlier
// bundle. selectParent chooses the concrete parent for this relationship, e.g.
// func(previous Models[Team,Project])Project{return previous.Child}.
func ThroughSelected[P, A, M, B any, K comparable](executor database.Executor, parent Resolver[P, A], selectParent func(A) M, relation query.ScopedRelation[M, B], field query.KeyField[B, K], key func(P) K) Resolver[P, Models[A, B]] {
	if nilValue(executor) || selectParent == nil || key == nil {
		return Resolver[P, Models[A, B]]{err: fault.New(fault.Invalid, "nested binding requires an executor and typed selectors")}
	}
	lookup := query.RelatedUnique(relation, field)
	result := Then(parent, func(ctx context.Context, path P, previous A) (value.Optional[B], error) {
		return lookup.Find(ctx, executor, selectParent(previous), key(path))
	})
	result.check = func() error {
		if err := parent.Validate(); err != nil {
			return err
		}
		return lookup.Validate()
	}
	return result
}
