package query

import (
	"database/sql/driver"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database/codec"
)

// Assignment is one model-owned mutation value. Ordinary consumers use generated
// drafts; Assign is the explicit declaration boundary used by generated code.
type Assignment[M any] struct {
	_     [0]*M
	field fieldRef
	assignmentValue
}

// assignmentValue is shared by model drafts and conflict literals. Keeping the
// captured value independent of M avoids re-instantiating its codec closure for
// every model/query scope while the public Assignment/Conflict retain ownership.
type assignmentValue struct {
	bind      func() (driver.Value, error)
	value     any // Private type erasure; generated metadata checks the exact V before mutation.
	copyValue func() any
}

// Assign captures a typed value and its codec. An omitted field produces no
// assignment; a nullable codec can bind explicit NULL. Duplicate columns fail.
func Assign[M any, V any](table, column string, c codec.Codec[V], v V) Assignment[M] {
	return Assignment[M]{field: fieldRef{table, column}, assignmentValue: captureAssignment(c, v)}
}

func captureAssignment[V any](c codec.Codec[V], v V) assignmentValue {
	v = c.Clone(v)
	return assignmentValue{value: v, copyValue: func() any { return c.Clone(v) }, bind: func() (driver.Value, error) { return c.Bind(c.Clone(v)) }}
}

func (v assignmentValue) inputValue() any {
	if v.copyValue != nil {
		return v.copyValue()
	}
	return v.value
}

// String and GoString keep pending values out of routine diagnostics.
func (Assignment[M]) String() string     { return "model assignment" }
func (a Assignment[M]) GoString() string { return a.String() }

// Mutation is an immutable, model-owned selection of explicitly assigned fields.
// It cannot be substituted for another model's mutation by implicit conversion.
type Mutation[M any] struct {
	_           [0]*M
	assignments []Assignment[M]
}

func (Mutation[M]) String() string     { return "model mutation" }
func (m Mutation[M]) GoString() string { return m.String() }

// Change constructs a mutation from generated assignments, copying their slice.
func Change[M any](assignments ...Assignment[M]) Mutation[M] {
	return Mutation[M]{assignments: slices.Clone(assignments)}
}

type mutationKind uint8

const (
	insertModel mutationKind = iota
	updateModel
	deleteModel
	softDeleteModel
	restoreModel
	forceDeleteModel
)

// mutationPlan uses the same expression AST, model metadata, codecs and compiler
// as reads. There is no model-specific SQL implementation.
type mutationPlan[M any] struct {
	query    Query[M]
	kind     mutationKind
	mutation Mutation[M]
	// setBased writes every row matching the query's effective predicates
	// in one statement: no primary-key predicate is required. countOnly omits
	// RETURNING; adjust adds column = column +/- delta.
	setBased  bool
	countOnly bool
	adjust    *adjustment
	// statementFailure, when set, receives the failure of the plan's own
	// statement, so InsertOrFirst can tell it from errors raised by hooks.
	statementFailure *error
}
