package query

import (
	"reflect"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type softDeleteScope uint8
type softDeleteColumn struct{ name string }

const (
	activeRecords softDeleteScope = iota
	allRecords
	trashedRecords
)

// WithSoftDeletes attaches the generated nullable deletion column. Applications
// declare DeletedAt on their model; they do not repeat column metadata here.
func (d Definition[M]) WithSoftDeletes(column string) Definition[M] {
	d.softDelete = &softDeleteColumn{name: column}
	return d
}

func (d Definition[M]) validateSoftDeletes() error {
	if d.softDelete == nil {
		return nil
	}
	field, ok := d.modelField(d.softDelete.name)
	column := slices.IndexFunc(d.columns, func(c Column) bool { return c.Name == d.softDelete.name })
	if !ok || column < 0 || !d.columns[column].Nullable || d.primary == d.softDelete.name || field.kind != codec.TypeDateTime || field.assignment == nil ||
		(field.typ != reflect.TypeFor[value.Nullable[time.Time]]() && field.typ != reflect.TypeFor[value.Nullable[temporal.DateTime]]()) {
		return fault.New(fault.Invalid, "soft deletion requires a declared nullable instant field")
	}
	if field.mutator.apply != nil && field.mutator.input != field.typ {
		return fault.New(fault.Invalid, "soft deletion cannot synthesize distinct mutator inputs")
	}
	return nil
}

func (q Query[M]) hasSoftDeletes() bool { return q.definition != nil && q.definition.softDelete != nil }

// WithTrashed includes active and soft-deleted models while preserving explicit
// predicates. It changes only this query; related models retain their own scope.
func (q Query[M]) WithTrashed() Query[M] { q.softDeleteScope = allRecords; return q }

// OnlyTrashed selects soft-deleted models while preserving explicit predicates.
// A query without soft-delete metadata rejects this option during validation.
func (q Query[M]) OnlyTrashed() Query[M] { q.softDeleteScope = trashedRecords; return q }

// WithoutTrashed restores the default active-model scope. It does not remove
// an explicitly supplied DeletedAt predicate or alter a reusable base query.
func (q Query[M]) WithoutTrashed() Query[M] { q.softDeleteScope = activeRecords; return q }

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

func (q Query[M]) validateSoftDeleteScope() error {
	if q.softDeleteScope > trashedRecords || (q.softDeleteScope != activeRecords && !q.hasSoftDeletes()) {
		return fault.New(fault.Invalid, "trashed visibility requires a soft-delete model declaration")
	}
	return nil
}

// effectivePredicates is the sole source of automatic model visibility. Keep
// explicit filters separate so replacing the implicit scope never removes one.
func (q Query[M]) effectivePredicates() []expression {
	if !q.hasSoftDeletes() || q.softDeleteScope == allRecords {
		return q.predicates
	}
	op := isNull
	if q.softDeleteScope == trashedRecords {
		op = isNotNull
	}
	return append(slices.Clone(q.predicates), q.deletionPredicate(op))
}

func (q Query[M]) deletionPredicate(op operator) expression {
	return comparison{operand: fieldRef{q.table, q.definition.softDelete.name}, operator: op}
}
