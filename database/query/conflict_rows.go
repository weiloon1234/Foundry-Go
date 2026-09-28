package query

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ConflictRow is the common expression scope of an upsert's stored and proposed
// records. It is distinct from the model's ordinary query scope.
type ConflictRow[M any] struct {
	_ [0]*M
	_ [0]struct{ conflict bool }
}

// ConflictRows exposes both records to generated FieldsAt accessors and explicit
// correlated subqueries. It has no standalone SELECT or execution operations.
type ConflictRows[M any] struct {
	record *recordMetadata[M]
	err    error
}

const conflictStoredTable = "foundry_conflict_stored"
const conflictProposedTable = "excluded"

// ConflictRows obtains model metadata for conflict calculations. Query filters
// do not become conflict conditions; use the policy's Where or WhereRows.
func (q Query[M]) ConflictRows() ConflictRows[M] {
	r := q.recordQuery()
	return ConflictRows[M]{record: r.metadata(), err: r.err}
}

// Stored supplies the existing conflicting row in the conflict expression scope.
func (r ConflictRows[M]) Stored() RecordScope[ConflictRow[M], M] {
	return r.recordScope(conflictStoredTable)
}

// Proposed supplies PostgreSQL's EXCLUDED row, including database defaults and
// BEFORE INSERT trigger changes.
func (r ConflictRows[M]) Proposed() RecordScope[ConflictRow[M], M] {
	return r.recordScope(conflictProposedTable)
}

func (r ConflictRows[M]) recordScope(table string) RecordScope[ConflictRow[M], M] {
	if r.err != nil || r.record == nil || len(r.record.columns) == 0 {
		return RecordScope[ConflictRow[M], M]{}
	}
	return RecordScope[ConflictRow[M], M]{table: table, record: r.record}
}

func (r ConflictRows[M]) scopeSource() queryScope[ConflictRow[M]] {
	scope := scopeRequirement{err: r.err}
	if r.record == nil || len(r.record.columns) == 0 {
		scope.err = fault.New(fault.Invalid, "conflict rows require complete model metadata")
	} else {
		scope.sources = map[string][]Column{
			conflictStoredTable:   r.record.columns,
			conflictProposedTable: r.record.columns,
		}
	}
	return queryScope[ConflictRow[M]]{scopeRequirement: scope}
}

// ConflictValueField is a generated destination field carrying its model and
// complete value type, including nullability. Computed values are not fields.
type ConflictValueField[M, V any] = ModelValueField[M, V]

// SetConflictValue assigns a calculation of the destination field's exact type.
// Obtain inputs from Query.ConflictRows and generated FieldsAt accessors. Use
// NullableRow to explicitly widen a non-null calculation for a nullable field.
// Selected aggregates/windows require a scalar subquery, preserving their phase.
func SetConflictValue[M, V any](field ConflictValueField[M, V], v RowValue[ConflictRow[M], V]) ConflictUpdate[M] {
	if nilDescriptor(field) {
		return ConflictUpdate[M]{}
	}
	return ConflictUpdate[M]{field: field.conflictColumn().field, value: rowInput(v).Value().node}
}

// WhereRows conditionally updates using both records. It is ANDed with existing
// Where/WhereRows clauses. False conditions skip RETURNING but retain row locks.
func (c Conflict[M]) WhereRows(predicates ...Predicate[ConflictRow[M]]) Conflict[M] {
	c.rowCondition = appendPredicates(c.rowCondition, predicates)
	return c
}

// ConflictScalarRowQuery evaluates an independent scalar SELECT in a conflict
// calculation. Zero rows become SQL NULL; multiple rows are a database error.
func ConflictScalarRowQuery[M, V any](rows ConflictRows[M], inner ValueQuerySource[V]) RowExpression[ConflictRow[M], value.Nullable[V]] {
	return RowExpression[ConflictRow[M], value.Nullable[V]]{scalarQuery[ConflictRow[M]](inner, rows.scopeSource().err)}
}

// ConflictScalarNullableRowQuery preserves one nullable layer for an independent
// scalar SELECT whose declared value is already nullable.
func ConflictScalarNullableRowQuery[M, V any](rows ConflictRows[M], inner ValueQuerySource[value.Nullable[V]]) RowExpression[ConflictRow[M], value.Nullable[V]] {
	return RowExpression[ConflictRow[M], value.Nullable[V]]{scalarNullableQuery[ConflictRow[M]](inner, rows.scopeSource().err)}
}
