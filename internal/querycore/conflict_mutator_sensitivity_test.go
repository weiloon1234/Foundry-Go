package querycore

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestConflictMutatorSensitivityPreservesProposedIdentity(t *testing.T) {
	field := FieldMutator{Column: "secret", Apply: func(a AssignmentValue) (AssignmentValue, error) {
		t.Fatal("identity validation invoked the mutator")
		return a, nil
	}}
	for _, tc := range []struct {
		name  string
		value ValueExpression
		valid bool
	}{
		{"own proposed", FieldRef{ConflictProposedTable, "secret"}, true},
		{"sensitive own proposed", SensitiveValue{Value: FieldRef{ConflictProposedTable, "secret"}}, true},
		{"sensitive foreign column", SensitiveValue{Value: FieldRef{ConflictProposedTable, "other"}}, false},
		{"sensitive stored value", SensitiveValue{Value: FieldRef{ConflictStoredTable, "secret"}}, false},
		{"sensitive parameter", SensitiveValue{Value: ParameterNode{}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateConflictMutator(ConflictUpdate{Field: FieldRef{"models", "secret"}, Value: tc.value}, field)
			if tc.valid && err != nil || !tc.valid && !errors.Is(err, fault.Invalid) {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
		})
	}
}
