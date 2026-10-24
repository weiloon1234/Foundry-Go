// Package database supplies advisory typed model validation. It reuses the
// query compiler and leaves the base validation package independent of SQL.
// Import it as databasevalidation alongside the database runtime package.
package database

import (
	"context"
	"fmt"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// Exists requires a stored value in the supplied model scope. The declared
// source and field must share a model owner, and input retains the field type.
// Example: databasevalidation.Exists(db, Users.Query(), Users.Key()).
func Exists[M any, V comparable](executor database.Executor, source orm.Query[M], field orm.KeyColumn[M, V]) validation.Rule[V] {
	return validation.Exists(Lookup(executor, source, field))
}

// Unique requires no matching value in the supplied model scope. For updates,
// use UniqueIgnoring, or exclude the current row with its typed primary-key
// predicate before passing the source. Include trashed rows when the database
// unique constraint does. This read does not reserve a value or replace the
// database constraint.
// Example: databasevalidation.Unique(db, Users.Query(), Users.Email).
func Unique[M any, V comparable](executor database.Executor, source orm.Query[M], field orm.KeyColumn[M, V]) validation.Rule[V] {
	return validation.Unique(Lookup(executor, source, field))
}

// ExistsAll validates a list with bounded database batches, preserving the
// model scope and concrete field type. Missing values are reported at their
// index paths. Duplicate input values are allowed; combine with Distinct when
// the application prohibits duplicates.
// Example: databasevalidation.ExistsAll(db, Users.Query(), Users.Key()).
func ExistsAll[M any, V comparable](executor database.Executor, source orm.Query[M], field orm.KeyColumn[M, V]) validation.Rule[[]V] {
	return validation.ExistsAll[[]V](Lookup(executor, source, field))
}

// UniqueAll requires that no listed value is stored in the scope, reporting
// each conflict at its index path with batched IN observations.
// Example: databasevalidation.UniqueAll(db, Users.Query(), Users.Email).
func UniqueAll[M any, V comparable](executor database.Executor, source orm.Query[M], field orm.KeyColumn[M, V]) validation.Rule[[]V] {
	return validation.UniqueAll[[]V](Lookup(executor, source, field))
}

// ExistsEach selects item from every element, for example each order line's
// product ID, and checks all selected values with batched statements. Missing
// values are reported at paths such as /items/3/product_id.
// Example: databasevalidation.ExistsEach(db, Users.Query(), Users.Key(), assignee).
func ExistsEach[E, M any, V comparable](executor database.Executor, source orm.Query[M], column orm.KeyColumn[M, V], item validation.Field[E, V]) validation.Rule[[]E] {
	return validation.ExistsEach[[]E](item, Lookup(executor, source, column))
}

// UniqueEach requires that no element's selected value is stored in the scope,
// reporting conflicts at each element's field path.
// Example: databasevalidation.UniqueEach(db, Users.Query(), Users.Email, email).
func UniqueEach[E, M any, V comparable](executor database.Executor, source orm.Query[M], column orm.KeyColumn[M, V], item validation.Field[E, V]) validation.Rule[[]E] {
	return validation.UniqueEach[[]E](item, Lookup(executor, source, column))
}

// UniqueIgnoring requires no other stored row with the value, excluding the row
// whose key the enclosing validation.Provide supplies through current, such as
// the route key of the record being updated. The exclusion is derived at check
// time, so one declaration serves every request. Validate rejects the rule
// until an enclosing Provide supplies current. Derive that key from trusted
// route or actor data, never from an untrusted body field alone.
// Example: databasevalidation.UniqueIgnoring(db, Users.Query(), Users.Email, Users.Key(), current).
func UniqueIgnoring[M any, V, K comparable](executor database.Executor, source orm.Query[M], field orm.KeyColumn[M, V], key interface {
	orm.KeyColumn[M, K]
	Ne(K) orm.Predicate[M]
}, current validation.Slot[K]) validation.Rule[V] {
	if isNil(key) {
		return validation.Requires(current, validation.Unique(newLookup(lookupState[M, V]{err: fault.New(fault.Invalid, "database validation requires a key field")})))
	}
	if err := orm.Lookup(source, key).Validate(); err != nil {
		return validation.Requires(current, validation.Unique(newLookup(lookupState[M, V]{err: err})))
	}
	// The fixed part of the scope is validated at declaration; only the
	// excluded key is supplied per check.
	if err := orm.Lookup(source, field).Validate(); err != nil {
		return validation.Requires(current, validation.Unique(newLookup(lookupState[M, V]{err: err})))
	}
	return validation.Requires(current, validation.Unique(Scoped(executor, func(ctx context.Context) (orm.Query[M], error) {
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
// Example: lookup := databasevalidation.Lookup(db, Users.Query(), Users.Email).
type ModelLookup[M any, V comparable] struct {
	_    [0]*M
	core *lookupState[M, V]
}

type lookupState[M any, V comparable] struct {
	executor database.Executor
	lookup   orm.ValueLookup[M, V]
	scoped   func(context.Context) (orm.ValueLookup[M, V], error)
	err      error
}

// Lookup binds a fixed declared model scope and field.
// Example: databasevalidation.Lookup(db, Users.Query(), Users.Email).
func Lookup[M any, V comparable](executor database.Executor, source orm.Query[M], field orm.KeyColumn[M, V]) ModelLookup[M, V] {
	return newLookup(lookupState[M, V]{executor: executor, lookup: orm.Lookup(source, field)})
}

// Scoped derives the model scope at every check, for example filtering by the
// tenant of the request's authenticated actor or by a validation.Slot value.
// The scope function must return a fresh unpaginated scope without ordering or
// eager loading; an invalid scope or a scope error fails execution and is never
// treated as a missing or available value. Declaration validation checks the
// executor and callback only; each check validates the returned scope.
// Example: databasevalidation.Scoped(db, tenantUsers, Users.Email).
func Scoped[M any, V comparable](executor database.Executor, scope func(context.Context) (orm.Query[M], error), field orm.KeyColumn[M, V]) ModelLookup[M, V] {
	if scope == nil || isNil(field) {
		return newLookup(lookupState[M, V]{err: fault.New(fault.Invalid, "database validation requires a scope and field")})
	}
	return newLookup(lookupState[M, V]{executor: executor, scoped: func(ctx context.Context) (orm.ValueLookup[M, V], error) {
		source, err := scope(ctx)
		if err != nil {
			return orm.ValueLookup[M, V]{}, err
		}
		lookup := orm.Lookup(source, field)
		return lookup, lookup.Validate()
	}})
}

// Validate checks the declaration without I/O. Example: lookup.Validate().
func (l ModelLookup[M, V]) Validate() error {
	if l.core == nil {
		return fault.New(fault.Invalid, "database validation requires an executor")
	}
	if l.core.err != nil {
		return l.core.err
	}
	if isNil(l.core.executor) {
		return fault.New(fault.Invalid, "database validation requires an executor")
	}
	if l.core.scoped != nil {
		return nil
	}
	return l.core.lookup.Validate()
}

func (l ModelLookup[M, V]) resolve(ctx context.Context) (orm.ValueLookup[M, V], error) {
	if err := l.Validate(); err != nil {
		return orm.ValueLookup[M, V]{}, err
	}
	if l.core.scoped == nil {
		return l.core.lookup, nil
	}
	return l.core.scoped(ctx)
}

// Exists observes one value. Example: lookup.Exists(ctx, input).
func (l ModelLookup[M, V]) Exists(ctx context.Context, input V) (bool, error) {
	lookup, err := l.resolve(ctx)
	if err != nil {
		return false, err
	}
	return lookup.Exists(ctx, l.core.executor, input)
}

// AllExist checks all supplied values in bounded batches. Example: lookup.AllExist(ctx, input).
func (l ModelLookup[M, V]) AllExist(ctx context.Context, input []V) (bool, error) {
	lookup, err := l.resolve(ctx)
	if err != nil {
		return false, err
	}
	return lookup.AllExist(ctx, l.core.executor, input)
}

// AnyExist checks for any stored conflict in bounded batches. Example: lookup.AnyExist(ctx, input).
func (l ModelLookup[M, V]) AnyExist(ctx context.Context, input []V) (bool, error) {
	lookup, err := l.resolve(ctx)
	if err != nil {
		return false, err
	}
	return lookup.AnyExist(ctx, l.core.executor, input)
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

func newLookup[M any, V comparable](state lookupState[M, V]) ModelLookup[M, V] {
	return ModelLookup[M, V]{core: &state}
}

// String redacts retained scope values. Example: fmt.Sprint(lookup).
func (ModelLookup[M, V]) String() string { return "database model lookup" }

// GoString redacts Go-syntax diagnostics. Example: fmt.Sprintf("%#v", lookup).
func (l ModelLookup[M, V]) GoString() string { return l.String() }

// Format redacts every formatting verb. Example: fmt.Sprintf("%+v", lookup).
func (l ModelLookup[M, V]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte(l.String())) }
