package querycore

import (
	"context"
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// ValidateConflictMutator prevents SQL calculations from bypassing a Go mutator.
// The field's own EXCLUDED value has already been transformed by the insert.
// Example: ValidateConflictMutator(update, field).
func ValidateConflictMutator(u ConflictUpdate, field FieldMutator) error {
	if field.Apply == nil {
		return nil
	}
	if u.Literal != nil {
		expected := field.Input
		if u.MutatedLiteral {
			expected = field.Output
		}
		if reflect.TypeOf(StoredInput(u.Literal.Value)) != expected {
			return fault.New(fault.Invalid, "conflict literal does not match its declared mutated field type")
		}
		return nil
	}
	if u.Incoming || u.Null {
		return nil
	}
	if source, ok := UnwrapSensitivity(u.Value).(FieldRef); ok && source.Table == ConflictProposedTable && source.Column == u.Field.Column {
		return nil
	}
	return fault.New(fault.Invalid, "conflict field with a Go mutator requires Set, SetNull, or its own proposed value")
}

// MutateConflictLiterals transforms a reusable policy once, independently of its
// number of proposed rows. Fields retain each facade's nullable input encoding.
// Example: MutateConflictLiterals(ctx, policy, fields).
func MutateConflictLiterals(ctx context.Context, policy Conflict, fields []FieldMutator) (Conflict, error) {
	var assignments []MutationAssignment
	for _, update := range policy.Updates {
		if update.Literal != nil {
			assignments = append(assignments, MutationAssignment{Field: update.Field, AssignmentValue: *update.Literal})
		}
	}
	if len(assignments) == 0 {
		return policy, nil
	}
	values, err := MutateAssignments(ctx, assignments, fields)
	if err != nil {
		return Conflict{}, err
	}
	policy.Updates = slices.Clone(policy.Updates)
	next := 0
	for i := range policy.Updates {
		if policy.Updates[i].Literal == nil {
			continue
		}
		policy.Updates[i].Literal = &values[next].AssignmentValue
		policy.Updates[i].MutatedLiteral = slices.ContainsFunc(fields, func(f FieldMutator) bool { return f.Column == policy.Updates[i].Field.Column && f.Apply != nil })
		next++
	}
	return policy, nil
}
