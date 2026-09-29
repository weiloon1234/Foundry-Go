// Package database supplies advisory typed model validation. It reuses the
// query compiler and leaves the base validation package independent of SQL.
// Import it as databasevalidation alongside the database runtime package.
package database

import (
	"context"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// Exists requires a stored value in the supplied model scope. The generated
// source and field must share a model owner, and input retains the field type.
func Exists[M any, V comparable](executor database.Executor, source query.ModelQuerySource[M], field query.KeyField[M, V]) validation.Rule[V] {
	return validation.Exists(Lookup(executor, source, field))
}

// Unique requires no matching value in the supplied model scope. For updates,
// use UniqueIgnoring, or exclude the current row with its typed primary-key
// predicate before passing the source. Include trashed rows when the database
// unique constraint does. This read does not reserve a value or replace the
// database constraint.
func Unique[M any, V comparable](executor database.Executor, source query.ModelQuerySource[M], field query.KeyField[M, V]) validation.Rule[V] {
	return validation.Unique(Lookup(executor, source, field))
}

// ExistsAll validates a list with bounded database batches, preserving the
// model scope and concrete field type. Missing values are reported at their
// index paths. Duplicate input values are allowed; combine with Distinct when
// the application prohibits duplicates.
func ExistsAll[M any, V comparable](executor database.Executor, source query.ModelQuerySource[M], field query.KeyField[M, V]) validation.Rule[[]V] {
	return validation.ExistsAll[[]V](Lookup(executor, source, field))
}

// UniqueAll requires that no listed value is stored in the scope, reporting
// each conflict at its index path with batched IN observations.
func UniqueAll[M any, V comparable](executor database.Executor, source query.ModelQuerySource[M], field query.KeyField[M, V]) validation.Rule[[]V] {
	return validation.UniqueAll[[]V](Lookup(executor, source, field))
}

// ExistsEach selects item from every element, for example each order line's
// product ID, and checks all selected values with batched statements. Missing
// values are reported at paths such as /items/3/product_id.
func ExistsEach[E, M any, V comparable](executor database.Executor, source query.ModelQuerySource[M], column query.KeyField[M, V], item validation.Field[E, V]) validation.Rule[[]E] {
	return validation.ExistsEach[[]E](item, Lookup(executor, source, column))
}

// UniqueEach requires that no element's selected value is stored in the scope,
// reporting conflicts at each element's field path.
func UniqueEach[E, M any, V comparable](executor database.Executor, source query.ModelQuerySource[M], column query.KeyField[M, V], item validation.Field[E, V]) validation.Rule[[]E] {
	return validation.UniqueEach[[]E](item, Lookup(executor, source, column))
}

// UniqueIgnoring requires no other stored row with the value, excluding the row
// whose key the enclosing validation.Provide supplies through current, such as
// the route key of the record being updated. The exclusion is derived at check
// time, so one declaration serves every request. Validate rejects the rule
// until an enclosing Provide supplies current. Derive that key from trusted
// route or actor data, never from an untrusted body field alone.
func UniqueIgnoring[M any, S interface {
	query.ModelQuerySource[M]
	Where(...query.Predicate[M]) S
}, V, K comparable](executor database.Executor, source S, field query.KeyField[M, V], key interface{ Ne(K) query.Predicate[M] }, current validation.Slot[K]) validation.Rule[V] {
	if isNil(key) {
		return validation.Requires(current, validation.Unique(ModelLookup[M, V]{err: fault.New(fault.Invalid, "database validation requires a key field")}))
	}
	// The fixed part of the scope is validated at declaration; only the
	// excluded key is supplied per check.
	if err := query.Lookup(source, field).Validate(); err != nil {
		return validation.Requires(current, validation.Unique(ModelLookup[M, V]{err: err}))
	}
	return validation.Requires(current, validation.Unique(Scoped(executor, func(ctx context.Context) (S, error) {
		ignored, err := current.Value(ctx)
		if err != nil {
			return source, err
		}
		return source.Where(key.Ne(ignored)), nil
	}, field)))
}

// ModelLookup observes one stored model field for the base validation rules:
// it implements validation.Lookup, BatchLookup and AnyLookup. Construct it with
// Lookup for a fixed scope or Scoped for a scope derived at check time.
type ModelLookup[M any, V comparable] struct {
	executor database.Executor
	lookup   query.ValueLookup[M, V]
	scoped   func(context.Context) (query.ValueLookup[M, V], error)
	err      error
}

// Lookup binds a fixed generated model scope and field.
func Lookup[M any, V comparable](executor database.Executor, source query.ModelQuerySource[M], field query.KeyField[M, V]) ModelLookup[M, V] {
	return ModelLookup[M, V]{executor: executor, lookup: query.Lookup(source, field)}
}

// Scoped derives the model scope at every check, for example filtering by the
// tenant of the request's authenticated actor or by a validation.Slot value.
// The scope function must return a fresh unpaginated scope without ordering or
// eager loading; an invalid scope or a scope error fails execution and is never
// treated as a missing or available value. Declaration validation checks the
// executor and callback only; each check validates the returned scope.
func Scoped[M any, S query.ModelQuerySource[M], V comparable](executor database.Executor, scope func(context.Context) (S, error), field query.KeyField[M, V]) ModelLookup[M, V] {
	if scope == nil || isNil(field) {
		return ModelLookup[M, V]{err: fault.New(fault.Invalid, "database validation requires a scope and field")}
	}
	return ModelLookup[M, V]{executor: executor, scoped: func(ctx context.Context) (query.ValueLookup[M, V], error) {
		source, err := scope(ctx)
		if err != nil {
			return query.ValueLookup[M, V]{}, err
		}
		lookup := query.Lookup(source, field)
		return lookup, lookup.Validate()
	}}
}

func (l ModelLookup[M, V]) Validate() error {
	if l.err != nil {
		return l.err
	}
	if isNil(l.executor) {
		return fault.New(fault.Invalid, "database validation requires an executor")
	}
	if l.scoped != nil {
		return nil
	}
	return l.lookup.Validate()
}

func (l ModelLookup[M, V]) resolve(ctx context.Context) (query.ValueLookup[M, V], error) {
	if l.scoped == nil {
		return l.lookup, nil
	}
	return l.scoped(ctx)
}

func (l ModelLookup[M, V]) Exists(ctx context.Context, input V) (bool, error) {
	lookup, err := l.resolve(ctx)
	if err != nil {
		return false, err
	}
	return lookup.Exists(ctx, l.executor, input)
}

func (l ModelLookup[M, V]) AllExist(ctx context.Context, input []V) (bool, error) {
	lookup, err := l.resolve(ctx)
	if err != nil {
		return false, err
	}
	return lookup.AllExist(ctx, l.executor, input)
}

func (l ModelLookup[M, V]) AnyExist(ctx context.Context, input []V) (bool, error) {
	lookup, err := l.resolve(ctx)
	if err != nil {
		return false, err
	}
	return lookup.AnyExist(ctx, l.executor, input)
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	ref := reflect.ValueOf(value)
	switch ref.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return ref.IsNil()
	}
	return false
}
