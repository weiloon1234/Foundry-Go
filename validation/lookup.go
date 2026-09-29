package validation

import (
	"context"
	"errors"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
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

// AnyLookup reports whether any value of a collection exists, in bounded
// batches. It backs UniqueAll and UniqueEach with the Lookup ownership contract.
type AnyLookup[T any] interface {
	Validate() error
	AnyExist(context.Context, []T) (bool, error)
}

// ExistsAll validates all values against one scoped source. Valid input costs
// one batched observation (split into the lookup's own statement batches). A
// rejection is located with bounded additional batch observations and reported
// at each missing element's index path, such as /tags/3, using the collection's
// label. The issue does not distinguish a nonexistent value from an inaccessible
// one. Every item is charged against the shared work budget before any I/O, and
// each observation costs one further check.
func ExistsAll[S ~[]T, T any](lookup BatchLookup[T]) Rule[S] {
	if lookup == nil {
		return failed[S](invalid("validation lookup is missing"))
	}
	if err := callback.Isolated("validation batch lookup declaration", lookup.Validate); err != nil {
		return failed[S](fault.Wrap(fault.Invalid, "invalid validation lookup", err))
	}
	return elementsRule[S](Spec{ID: "foundry.exists_all"}, nil, func(item T) T { return item }, lookup.AllExist)
}

// UniqueAll requires that no value of a collection exists in the scoped source,
// reporting each conflicting element at its index path. It does not detect
// duplicates inside the request itself; combine it with Distinct.
func UniqueAll[S ~[]T, T any](lookup AnyLookup[T]) Rule[S] {
	if lookup == nil {
		return failed[S](invalid("validation lookup is missing"))
	}
	if err := callback.Isolated("validation batch lookup declaration", lookup.Validate); err != nil {
		return failed[S](fault.Wrap(fault.Invalid, "invalid validation lookup", err))
	}
	return elementsRule[S](Spec{ID: "foundry.unique_all"}, nil, func(item T) T { return item }, noneExist(lookup))
}

// ExistsEach selects one field from every object of a collection and checks the
// selected values with batched observations instead of one call per element.
// Missing values are reported at the element's field path, for example
// /items/3/product_id, using that field's label.
func ExistsEach[S ~[]E, E, V any](field Field[E, V], lookup BatchLookup[V]) Rule[S] {
	if err := field.Validate(); err != nil {
		return failed[S](err)
	}
	if lookup == nil {
		return failed[S](invalid("validation lookup is missing"))
	}
	if err := callback.Isolated("validation batch lookup declaration", lookup.Validate); err != nil {
		return failed[S](fault.Wrap(fault.Invalid, "invalid validation lookup", err))
	}
	return elementsRule[S](Spec{ID: "foundry.exists", Parameters: []Parameter{parameter("field", field.name)}}, &field, field.selectValue, lookup.AllExist)
}

// UniqueEach selects one field from every object and requires that none of the
// selected values exists in the scoped source, reporting conflicts at the
// element's field path. It is advisory and does not reserve values; combine it
// with DistinctBy for duplicates inside the request.
func UniqueEach[S ~[]E, E, V any](field Field[E, V], lookup AnyLookup[V]) Rule[S] {
	if err := field.Validate(); err != nil {
		return failed[S](err)
	}
	if lookup == nil {
		return failed[S](invalid("validation lookup is missing"))
	}
	if err := callback.Isolated("validation batch lookup declaration", lookup.Validate); err != nil {
		return failed[S](fault.Wrap(fault.Invalid, "invalid validation lookup", err))
	}
	return elementsRule[S](Spec{ID: "foundry.unique", Parameters: []Parameter{parameter("field", field.name)}}, &field, field.selectValue, noneExist(lookup))
}

func noneExist[T any](lookup AnyLookup[T]) func(context.Context, []T) (bool, error) {
	return func(ctx context.Context, values []T) (bool, error) {
		found, err := lookup.AnyExist(ctx, values)
		return !found, err
	}
}

// elementsRule reports each rejected element of a collection. observe returns
// true when every value of a batch passes and owns the slice it receives.
func elementsRule[S ~[]E, E, V any](spec Spec, field *Field[E, V], pick func(E) V, observe func(context.Context, []V) (bool, error)) Rule[S] {
	rule := valueRule(spec, true, func(*execution, S) (bool, error) { return true, nil })
	if rule.err != nil {
		return rule
	}
	rule.leaf, rule.callbacks = nil, true
	rule.report = func(s *execution, input S, spec Spec, message *i18n.PreparedMessage) error {
		values := make([]V, 0, len(input))
		for _, item := range input {
			if !s.take(0) {
				return nil
			}
			values = append(values, pick(item))
		}
		if len(values) == 0 {
			return nil
		}
		return locate(s, values, observe, func(index int) bool {
			s.enterIndex(index)
			if field != nil {
				previous, previousKey, previousField := s.label, s.labelKey, s.field
				s.label, s.labelKey, s.field = field.label, field.labelKey, field.name
				s.enter(field.name)
				s.issue(spec, message)
				s.leave()
				s.label, s.labelKey, s.field = previous, previousKey, previousField
			} else {
				s.issue(spec, message)
			}
			s.leave()
			if s.err != nil {
				return false
			}
			if len(s.issues) >= s.limits.Issues {
				s.truncated = true
				return false
			}
			return true
		})
	}
	return rule
}

var errStopped = errors.New("validation location stopped")

// locate observes all values once. Only when that batch rejects does it bisect
// to the rejected positions: a half that passes proves the other half holds a
// rejection, so k rejected values among n cost about 2k·log2(n/k) observations.
// Each observation receives an owned copy and is charged one unit of work.
func locate[V any](s *execution, values []V, observe func(context.Context, []V) (bool, error), report func(int) bool) error {
	check := func(batch []V) (bool, error) {
		if !s.take(0) {
			return false, errStopped
		}
		return observe(s.ctx, slices.Clone(batch))
	}
	passed, err := check(values)
	if err == nil && !passed {
		_, err = bisect(check, report, values, 0)
	}
	if errors.Is(err, errStopped) {
		return nil
	}
	return err
}

// bisect narrows a batch known to contain a rejected value. It returns false
// when reporting stopped at the issue cap or an execution failure.
func bisect[V any](check func([]V) (bool, error), report func(int) bool, values []V, offset int) (bool, error) {
	if len(values) == 1 {
		return report(offset), nil
	}
	half := len(values) / 2
	leftPassed, err := check(values[:half])
	if err != nil {
		return false, err
	}
	if !leftPassed {
		if more, err := bisect(check, report, values[:half], offset); !more || err != nil {
			return false, err
		}
		rightPassed, err := check(values[half:])
		if err != nil || rightPassed {
			return err == nil, err
		}
	}
	return bisect(check, report, values[half:], offset+half)
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
	rule := leaf(spec, true, func(ctx context.Context, input T) (bool, error) {
		found, err := lookup.Exists(ctx, input)
		return found == wantExists, err
	})
	rule.callbacks = rule.err == nil
	return rule
}
