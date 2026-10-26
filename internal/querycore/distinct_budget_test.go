package querycore

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestDistinctOrderMatchingIsBounded(t *testing.T) {
	field := FieldRef{Table: "records", Column: "id"}
	scalar := ScalarSubquery{Query: Subquery{Node: SelectNode{Source: TableSource{Table: "records", Columns: []Column{{Name: "id"}}}, Selections: []SelectItem{{Expression: field}}, Predicates: []Expression{BinaryComparison{Left: field, Right: ParameterNode{Kind: codec.TypeInteger, Value: RetainLiteral(int64(1))}, Operator: Equal}}}}}
	c := Compiler{Sources: map[string]map[string]Column{"records": {"id": {Name: "id"}}}}
	plan, err := c.PlanValue(scalar)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                   string
		comparisons, arguments int
		invalid                bool
	}{
		{"within bounds", MaxExpressionNodes - 1, MaxParameters - 1, false},
		{"expression budget", MaxExpressionNodes, 0, true},
		{"binding budget", 0, MaxParameters, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := DistinctOrder{Index: ValueIndex{Comparisons: tc.comparisons, Arguments: tc.arguments}}
			d.Add(plan, 1)
			got, err := d.Compile(&c, OrderNode{Expression: scalar}, nil, false)
			if tc.invalid {
				if !errors.Is(err, fault.Invalid) {
					t.Fatal("matching budget ignored", err)
				}
			} else if err != nil || got != "1" {
				t.Fatal(got, err)
			}
		})
	}
}
