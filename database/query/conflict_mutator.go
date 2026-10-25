package query

import (
	"context"
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// A Go mutator cannot transform an unevaluated SQL expression. A literal uses
// the shared assignment pipeline; the destination's own proposed field already
// passed through that pipeline and must not be transformed twice.
func (q Query[M]) validateConflictMutator(u conflictUpdate) error {
	field, declared := q.definition.modelField(u.Field.Column)
	if !declared || field.conflictMutator.apply == nil {
		return nil
	}
	if u.Literal != nil {
		expected := field.conflictMutator.input
		if u.MutatedLiteral {
			expected = field.conflictMutator.output
		}
		if reflect.TypeOf(u.Literal.Value) != expected {
			return safeerror.NewFault(fault.Invalid, "conflict literal does not match its declared mutated field type")
		}
		return nil
	}
	if u.Incoming || u.Null {
		return nil
	}
	if source, ok := u.Value.(fieldRef); ok && source.Table == conflictProposedTable && source.Column == u.Field.Column {
		return nil
	}
	return safeerror.NewFault(fault.Invalid, "conflict field with a Go mutator requires Set, SetNull, or its own proposed value")
}

// mutateLiterals transforms one shared conflict policy, independently of the
// number of proposed rows. The caller's policy and assignments remain reusable.
func (p Conflict[M]) mutateLiterals(ctx context.Context, q Query[M]) (Conflict[M], error) {
	var assignments []Assignment[M]
	for _, update := range p.state.Updates {
		if update.Literal != nil {
			assignments = append(assignments, Assignment[M]{mutationAssignment: mutationAssignment{Field: update.Field, AssignmentValue: *update.Literal}})
		}
	}
	if len(assignments) == 0 {
		return p, nil
	}
	mutation, err := q.mutateAssignments(ctx, Change(assignments...), true)
	if err != nil {
		return Conflict[M]{}, err
	}
	p.state.Updates = slices.Clone(p.state.Updates)
	next := 0
	for i := range p.state.Updates {
		if p.state.Updates[i].Literal != nil {
			p.state.Updates[i].Literal = &mutation.assignments[next].AssignmentValue
			if field, ok := q.definition.modelField(p.state.Updates[i].Field.Column); ok && field.conflictMutator.apply != nil {
				p.state.Updates[i].MutatedLiteral = true
			}
			next++
		}
	}
	return p, nil
}
