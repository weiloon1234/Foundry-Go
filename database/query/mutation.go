package query

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

type mutationAssignment = querycore.MutationAssignment

type assignmentValue = querycore.AssignmentValue

type mutationKind = querycore.MutationKind

const insertModel = querycore.InsertModel

const updateModel = querycore.UpdateModel

const deleteModel = querycore.DeleteModel

const softDeleteModel = querycore.SoftDeleteModel

const restoreModel = querycore.RestoreModel

const forceDeleteModel = querycore.ForceDeleteModel

// Assignment is one model-owned mutation value. Ordinary consumers use generated
// drafts; Assign is the explicit declaration boundary used by generated code.
type Assignment[M any] struct {
	_                  [0]*M
	mutationAssignment mutationAssignment
}

// Assign captures a typed value and its codec. An omitted field produces no
// assignment; a nullable codec can bind explicit NULL. Duplicate columns fail.
func Assign[M any, V any](table, column string, c codec.Codec[V], v V) Assignment[M] {
	return Assignment[M]{mutationAssignment: mutationAssignment{Field: fieldRef{Table: table, Column: column}, AssignmentValue: captureAssignment(c, v)}}
}

func captureAssignment[V any](c codec.Codec[V], v V) assignmentValue {
	return querycore.CaptureAssignment(c, v)
}

// String and GoString keep pending values out of routine diagnostics.
func (Assignment[M]) String() string { return "model assignment" }

func (a Assignment[M]) GoString() string { return a.String() }

// Mutation is an immutable, model-owned selection of explicitly assigned fields.
// It cannot be substituted for another model's mutation by implicit conversion.
type Mutation[M any] struct {
	_           [0]*M
	assignments []mutationAssignment
}

func (Mutation[M]) String() string { return "model mutation" }

func (m Mutation[M]) GoString() string { return m.String() }

// Change constructs a mutation from generated assignments, copying their slice.
func Change[M any](assignments ...Assignment[M]) Mutation[M] {
	lowered := make([]mutationAssignment, len(assignments))
	for i, a := range assignments {
		lowered[i] = a.mutationAssignment
	}
	return Mutation[M]{assignments: lowered}
}

// observedBy reports whether observers registered for M take part in a write
// of kind; deletion observers take part only in deletions.
func observedBy[M any](set lifecycle.Observers, kind mutationKind) bool {
	if kind.Deletes() {
		return lifecycle.HasDeletionObservers[M](set)
	}
	return lifecycle.HasObservers[M](set)
}

// observersFor is the observer set a write of kind dispatches.
func observersFor(set lifecycle.Observers, kind mutationKind) lifecycle.Observers {
	if kind.Deletes() {
		return set.ForDeletion()
	}
	return set
}

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

func (m Mutation[M]) clone() Mutation[M] {
	m.assignments = slices.Clone(m.assignments)
	return m
}
