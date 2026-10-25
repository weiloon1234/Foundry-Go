package query

import (
	"reflect"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type softDeleteScope = querycore.SoftDeleteScope

type softDeleteColumn = querycore.SoftDeleteColumn

const activeRecords = querycore.ActiveRecords

const allRecords = querycore.AllRecords

const trashedRecords = querycore.TrashedRecords

// WithSoftDeletes attaches the generated nullable deletion column. Applications
// declare DeletedAt on their model; they do not repeat column metadata here.
func (d Definition[M]) WithSoftDeletes(column string) Definition[M] {
	d.modelDefinition.SoftDelete = &softDeleteColumn{Name: column}
	return d
}

func (d Definition[M]) validateSoftDeletes() error {
	if d.modelDefinition.SoftDelete == nil {
		return nil
	}
	field, ok := d.modelField(d.modelDefinition.SoftDelete.Name)
	return validateSoftDeleteField(d.modelDefinition, field.recordFieldMetadata, ok)
}

func validateSoftDeleteField(model modelDefinition, field recordFieldMetadata, declared bool) error {
	column := slices.IndexFunc(model.Columns, func(c columnData) bool { return c.Name == model.SoftDelete.Name })
	if !declared || column < 0 || !model.Columns[column].Nullable || model.Primary == model.SoftDelete.Name || field.kind != codec.TypeDateTime || field.assignment == nil ||
		(field.typ != reflect.TypeFor[value.Nullable[time.Time]]() && field.typ != reflect.TypeFor[value.Nullable[temporal.DateTime]]()) {
		return safeerror.NewFault(fault.Invalid, "soft deletion requires a declared nullable instant field")
	}
	if field.mutator.apply != nil && field.mutator.input != field.typ {
		return safeerror.NewFault(fault.Invalid, "soft deletion cannot synthesize distinct mutator inputs")
	}
	return nil
}

// WithTrashed includes active and soft-deleted models while preserving explicit
// predicates. It changes only this query; related models retain their own scope.
func (q Query[M]) WithTrashed() Query[M] { q.queryCore.SoftDeleteScope = allRecords; return q }

// OnlyTrashed selects soft-deleted models while preserving explicit predicates.
// A query without soft-delete metadata rejects this option during validation.
func (q Query[M]) OnlyTrashed() Query[M] { q.queryCore.SoftDeleteScope = trashedRecords; return q }

// WithoutTrashed restores the default active-model scope. It does not remove
// an explicitly supplied DeletedAt predicate or alter a reusable base query.
func (q Query[M]) WithoutTrashed() Query[M] { q.queryCore.SoftDeleteScope = activeRecords; return q }

// WithTrashed includes deleted models without changing transaction lock policy.
func (q LockedQuery[M]) WithTrashed() LockedQuery[M] {
	q.query = q.query.WithTrashed()
	return q
}

// OnlyTrashed selects deleted models without changing transaction lock policy.
func (q LockedQuery[M]) OnlyTrashed() LockedQuery[M] {
	q.query = q.query.OnlyTrashed()
	return q
}

// WithoutTrashed restores active-model visibility while retaining the lock.
func (q LockedQuery[M]) WithoutTrashed() LockedQuery[M] {
	q.query = q.query.WithoutTrashed()
	return q
}
