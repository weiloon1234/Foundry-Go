package query

import (
	"context"
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// A Go mutator cannot transform an unevaluated SQL expression. A literal uses
// the shared assignment pipeline; the destination's own proposed field already
// passed through that pipeline and must not be transformed twice.
func (u ConflictUpdate[M]) validateMutator(q Query[M]) error {
	field, declared := q.definition.modelField(u.field.Column)
	if !declared || field.conflictMutator.apply == nil {
		return nil
	}
	if u.literal != nil {
		expected := field.conflictMutator.input
		if u.mutatedLiteral {
			expected = field.conflictMutator.output
		}
		if reflect.TypeOf(u.literal.Value) != expected {
			return fault.New(fault.Invalid, "conflict literal does not match its declared mutated field type")
		}
		return nil
	}
	if u.incoming || u.null {
		return nil
	}
	if source, ok := u.value.(fieldRef); ok && source.Table == conflictProposedTable && source.Column == u.field.Column {
		return nil
	}
	return fault.New(fault.Invalid, "conflict field with a Go mutator requires Set, SetNull, or its own proposed value")
}

// mutateLiterals transforms one shared conflict policy, independently of the
// number of proposed rows. The caller's policy and assignments remain reusable.
func (p Conflict[M]) mutateLiterals(ctx context.Context, q Query[M]) (Conflict[M], error) {
	var assignments []Assignment[M]
	for _, update := range p.updates {
		if update.literal != nil {
			assignments = append(assignments, Assignment[M]{mutationAssignment: mutationAssignment{Field: update.field, AssignmentValue: *update.literal}})
		}
	}
	if len(assignments) == 0 {
		return p, nil
	}
	mutation, err := q.mutateAssignments(ctx, Change(assignments...), true)
	if err != nil {
		return Conflict[M]{}, err
	}
	p.updates = slices.Clone(p.updates)
	next := 0
	for i := range p.updates {
		if p.updates[i].literal != nil {
			p.updates[i].literal = &mutation.assignments[next].AssignmentValue
			if field, ok := q.definition.modelField(p.updates[i].field.Column); ok && field.conflictMutator.apply != nil {
				p.updates[i].mutatedLiteral = true
			}
			next++
		}
	}
	return p, nil
}
