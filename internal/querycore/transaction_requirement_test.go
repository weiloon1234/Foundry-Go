package querycore

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestTransactionRequirementSurvivesSourceLowering(t *testing.T) {
	columns := []Column{{Name: "id"}}
	ordinary := SelectNode{Source: TableSource{Table: "items", Columns: columns}, Selections: ColumnSelections("items", columns)}
	required := ordinary
	required.Source.RequiresTransaction = true
	check := func(t *testing.T, node SelectNode) {
		t.Helper()
		if statement, err := CompileRead(node, t.Context(), false, nil, true); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "transaction-scoped") || statement.SQLText != "" {
			t.Fatal("transaction requirement lost", statement, err)
		}
		if _, err := CompileRead(node, t.Context(), true, nil, true); err != nil {
			t.Fatal("valid transaction rejected", err)
		}
	}
	type left struct{}
	type right struct{}
	for _, direct := range []bool{true, false} {
		for _, side := range []string{"left", "right"} {
			for kind := InnerJoin; kind <= CrossJoin; kind++ {
				t.Run(fmt.Sprintf("direct=%v/%s/kind=%d", direct, side, kind), func(t *testing.T) {
					l, r := ordinary, ordinary
					if side == "left" {
						l = required
					} else {
						r = required
					}
					a := AliasNode(l, columns, direct, "a", reflect.TypeFor[left](), nil)
					b := AliasNode(r, columns, direct, "b", reflect.TypeFor[right](), nil)
					var on Expression = BinaryComparison{Left: FieldRef{Table: "a", Column: "id"}, Right: FieldRef{Table: "b", Column: "id"}, Operator: Equal}
					if kind == CrossJoin {
						on = nil
					}
					joined := JoinNodes(a.Node, b.Node, a.Aliases, b.Aliases, a.Err, b.Err, on, kind)
					if joined.Err != nil {
						t.Fatal(joined.Err)
					}
					check(t, joined.Node)
				})
			}
		}
	}
	for _, name := range []string{"IN", "EXISTS", "scalar", "CTE", "set_left", "set_right"} {
		t.Run(name, func(t *testing.T) {
			node := ordinary
			switch name {
			case "IN":
				node.Predicates = []Expression{SubqueryPredicate{Query: Subquery{Node: required}, Operand: FieldRef{Table: "items", Column: "id"}}}
			case "EXISTS":
				node.Predicates = []Expression{SubqueryPredicate{Query: Subquery{Node: required}}}
			case "scalar":
				node.Selections = []SelectItem{{Expression: ScalarSubquery{Query: Subquery{Node: required}}}}
			case "CTE":
				node = SelectNode{Source: TableSource{Cte: &CteNode{Name: "selected", Query: required, Columns: columns}, Columns: columns}}
			default:
				a, b := ordinary, required
				if name == "set_left" {
					a, b = b, a
				}
				node = SelectNode{Source: TableSource{Set: &SetNode{Left: a, Right: b, Operator: UnionAllSet}, Alias: SetAlias, Columns: columns}}
			}
			check(t, node)
		})
	}
	// Marking a copied node must not restrict the original query.
	if _, err := CompileRead(ordinary, t.Context(), false, nil, true); err != nil {
		t.Fatal("transaction lift mutated ordinary source", err)
	}
}
