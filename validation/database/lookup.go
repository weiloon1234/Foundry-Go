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
	return validation.Exists(modelLookup[M, V]{executor: executor, lookup: query.Lookup(source, field)})
}

// Unique requires no matching value in the supplied model scope. For updates,
// exclude the current row with its typed primary-key predicate before passing
// the source. Include trashed rows when the database unique constraint does.
// This read does not reserve a value or replace the database constraint.
func Unique[M any, V comparable](executor database.Executor, source query.ModelQuerySource[M], field query.KeyField[M, V]) validation.Rule[V] {
	return validation.Unique(modelLookup[M, V]{executor: executor, lookup: query.Lookup(source, field)})
}

// ExistsAll validates a list with bounded database batches, preserving the
// model scope and concrete field type. Duplicate input values are allowed;
// combine with Distinct when the application prohibits duplicates.
func ExistsAll[M any, V comparable](executor database.Executor, source query.ModelQuerySource[M], field query.KeyField[M, V]) validation.Rule[[]V] {
	return validation.ExistsAll[[]V](modelLookup[M, V]{executor: executor, lookup: query.Lookup(source, field)})
}

type modelLookup[M any, V comparable] struct {
	executor database.Executor
	lookup   query.ValueLookup[M, V]
}

func (l modelLookup[M, V]) Validate() error {
	if l.executor == nil {
		return fault.New(fault.Invalid, "database validation requires an executor")
	}
	ref := reflect.ValueOf(l.executor)
	switch ref.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if ref.IsNil() {
			return fault.New(fault.Invalid, "database validation requires an executor")
		}
	}
	return l.lookup.Validate()
}

func (l modelLookup[M, V]) Exists(ctx context.Context, input V) (bool, error) {
	return l.lookup.Exists(ctx, l.executor, input)
}

func (l modelLookup[M, V]) AllExist(ctx context.Context, input []V) (bool, error) {
	return l.lookup.AllExist(ctx, l.executor, input)
}
