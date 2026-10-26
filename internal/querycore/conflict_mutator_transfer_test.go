package querycore

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"testing"
)

func TestConflictMutatorsRejectComputedBypassBeforeUserCode(t *testing.T) {
	calls := 0
	field := NewFieldMutator("name", codec.String[string](), func(s string) (string, error) { calls++; return s, nil })
	proposed := FieldRef{Table: ConflictProposedTable, Column: "name"}
	stored := FieldRef{Table: ConflictStoredTable, Column: "name"}
	for _, input := range []ValueExpression{stored, OperationNode{Kind: LowerOperation, Result: codec.TypeText, Arguments: []OperationArgument{{Value: proposed, Kind: codec.TypeText}}}, FieldRef{Table: ConflictProposedTable, Column: "other"}, ParameterNode{Kind: codec.TypeText, Value: RetainLiteral("literal expression")}} {
		if e := ValidateConflictMutator(ConflictUpdate{Field: FieldRef{Table: "users", Column: "name"}, Value: input}, field); !errors.Is(e, fault.Invalid) || calls != 0 {
			t.Fatal("computed mutator bypass", e, calls)
		}
	}
	for _, u := range []ConflictUpdate{{Field: FieldRef{Table: "users", Column: "name"}, Value: proposed}, {Incoming: true}, {Null: true}} {
		if e := ValidateConflictMutator(u, field); e != nil || calls != 0 {
			t.Fatal("already transformed proposal or NULL", e, calls)
		}
	}
}
