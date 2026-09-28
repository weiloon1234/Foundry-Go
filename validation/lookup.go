package validation

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// Lookup checks whether a concrete value exists in an explicitly scoped source.
// Validate checks its declaration without I/O. Exists must honor the context and
// retain ownership of its work until return. The database adapter supplies this
// contract from generated queries; applications can provide other typed sources.
type Lookup[T any] interface {
	Validate() error
	Exists(context.Context, T) (bool, error)
}

// BatchLookup checks a collection in bounded batches rather than one remote
// call per value. Implementations retain the same error/ownership contract as
// Lookup. Empty input succeeds without I/O; use MinItems to require selection.
type BatchLookup[T any] interface {
	Validate() error
	AllExist(context.Context, []T) (bool, error)
}

// ExistsAll validates all values against one scoped source. It reports at the
// collection path, does not reveal which inaccessible value is missing, and
// charges every item against the shared work budget before starting I/O.
func ExistsAll[S ~[]T, T any](lookup BatchLookup[T]) Rule[S] {
	if lookup == nil {
		return failed[S](invalid("validation lookup is missing"))
	}
	if err := callback.Isolated("validation batch lookup declaration", lookup.Validate); err != nil {
		return failed[S](fault.Wrap(fault.Invalid, "invalid validation lookup", err))
	}
	return valueRule(Spec{ID: "foundry.exists_all"}, true, func(s *execution, input S) (bool, error) {
		for range input {
			if !s.take(0) {
				return false, nil
			}
		}
		if len(input) == 0 {
			return true, nil
		}
		return lookup.AllExist(s.ctx, slices.Clone([]T(input)))
	})
}

// Exists requires a matching value in the declared lookup. It is server-only
// and advisory: later changes can invalidate the observation. Input omission
// and nullability are handled by the normal Optional/Nullable adapters.
func Exists[T any](lookup Lookup[T]) Rule[T] { return lookupRule(lookup, true) }

// Unique requires no matching value in the declared lookup. It does not reserve
// the value or replace a database unique constraint. A lookup failure is an
// internal execution failure, never evidence that the value is available.
func Unique[T any](lookup Lookup[T]) Rule[T] { return lookupRule(lookup, false) }

func lookupRule[T any](lookup Lookup[T], wantExists bool) Rule[T] {
	if lookup == nil {
		return failed[T](invalid("validation lookup is missing"))
	}
	if err := callback.Isolated("validation lookup declaration", lookup.Validate); err != nil {
		return failed[T](fault.Wrap(fault.Invalid, "invalid validation lookup", err))
	}
	spec := Spec{ID: "foundry.exists"}
	if !wantExists {
		spec = Spec{ID: "foundry.unique"}
	}
	return leaf(spec, true, func(ctx context.Context, input T) (bool, error) {
		found, err := lookup.Exists(ctx, input)
		return found == wantExists, err
	})
}
