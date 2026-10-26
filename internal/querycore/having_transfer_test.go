package querycore

import (
	"database/sql/driver"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
	"math"
	"reflect"
	"strings"
	"testing"
)

func havingTransferNode() SelectNode {
	id := FieldRef{Table: "records", Column: "id"}
	return SelectNode{Source: TableSource{Table: "records", Columns: []Column{{Name: "id"}, {Name: "label"}}}, Selections: []SelectItem{{Expression: id, Alias: "id"}, {Expression: AggregateNode{Kind: CountAll}, Alias: "count"}}, GroupBy: []ValueExpression{id}}
}
func havingTransferComparison(left ValueExpression, operator Operator, kind codec.ParameterType, v any) Expression {
	return Comparison{Operand: left, Operator: operator, Kind: kind, Values: RetainComparisonValues([]any{v}), Bind: func(v any) (driver.Value, error) {
		if kind == codec.TypeFloat {
			return codec.Float[float64]().Bind(v.(float64))
		}
		return v, nil
	}}
}
func TestHavingAndOrderingImplyGrouping(t *testing.T) {
	node := havingTransferNode()
	node.GroupBy = nil
	node.Selections = node.Selections[:1]
	for _, derive := range []func(SelectNode) SelectNode{
		func(n SelectNode) SelectNode {
			n.Orders = []OrderNode{{Expression: AggregateNode{Kind: CountAll}}}
			return n
		},
		func(n SelectNode) SelectNode {
			n.Having = []Expression{havingTransferComparison(FieldRef{Table: "records", Column: "id"}, Equal, codec.TypeInteger, int64(1))}
			return n
		},
	} {
		if _, err := (&Compiler{}).SelectSQL(derive(node)); !errors.Is(err, fault.Invalid) {
			t.Fatal("ungrouped field accepted", err)
		}
	}
}
func TestHavingRejectsInvalidScopeStructureAndBindings(t *testing.T) {
	base := havingTransferNode()
	id := FieldRef{Table: "records", Column: "id"}
	count := AggregateNode{Kind: CountAll}
	for name, change := range map[string]func(*SelectNode){
		"zero": func(n *SelectNode) { n.Having = []Expression{nil} },
		"ungrouped": func(n *SelectNode) {
			n.Having = []Expression{havingTransferComparison(FieldRef{Table: "records", Column: "label"}, Equal, codec.TypeText, "not grouped")}
		},
		"foreign aggregate": func(n *SelectNode) {
			n.Having = []Expression{havingTransferComparison(AggregateNode{Kind: SumValues, Cast: ExactNumberCast, Field: FieldRef{Table: "other", Column: "id"}}, Greater, codec.TypeDecimal, "1")}
		},
		"nonfinite": func(n *SelectNode) {
			n.Having = []Expression{havingTransferComparison(AggregateNode{Kind: AverageValues, Cast: FloatNumberCast, Field: id}, Greater, codec.TypeFloat, math.Inf(1))}
		},
		"zero order": func(n *SelectNode) { n.Orders = []OrderNode{{}} },
		"foreign order": func(n *SelectNode) {
			n.Orders = []OrderNode{{Expression: AggregateNode{Kind: SumValues, Cast: ExactNumberCast, Field: FieldRef{Table: "other", Column: "id"}}}}
		},
		"aggregate where": func(n *SelectNode) {
			n.Predicates = []Expression{havingTransferComparison(count, Equal, codec.TypeInteger, int64(1))}
		},
		"depth": func(n *SelectNode) {
			var e Expression = havingTransferComparison(count, Equal, codec.TypeInteger, int64(1))
			for range MaxExpressionDepth + 1 {
				e = Negation{Child: e}
			}
			n.Having = []Expression{e}
		},
		"shared nodes": func(n *SelectNode) {
			n.Predicates = []Expression{havingTransferComparison(id, Equal, codec.TypeInteger, int64(1))}
			n.Having = make([]Expression, MaxExpressionNodes)
			for i := range n.Having {
				n.Having[i] = havingTransferComparison(count, Equal, codec.TypeInteger, int64(1))
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			node := base
			change(&node)
			if _, e := CompileRead(node, t.Context(), false, nil, true); !errors.Is(e, fault.Invalid) {
				t.Fatal(e)
			}
		})
	}
}
func TestHavingAndAggregateOrderingShareSelectCompiler(t *testing.T) {
	node := havingTransferNode()
	id := FieldRef{Table: "records", Column: "id"}
	count := AggregateNode{Kind: CountAll}
	sum := AggregateNode{Kind: SumValues, Cast: ExactNumberCast, Field: id}
	values := []any{int64(1), int64(2)}
	membership := Comparison{Operand: id, Operator: In, Kind: codec.TypeInteger, Values: RetainComparisonValues(values), Bind: func(v any) (driver.Value, error) { return v, nil }}
	node.Predicates = []Expression{havingTransferComparison(id, Greater, codec.TypeInteger, int64(0))}
	node.Having = []Expression{Junction{Children: []Expression{havingTransferComparison(count, Greater, codec.TypeInteger, int64(2)), Junction{Any: true, Children: []Expression{havingTransferComparison(sum, Greater, codec.TypeDecimal, "5"), Negation{Child: membership}}}}}}
	node.Orders = []OrderNode{{Expression: count, Descending: true}, {Expression: id}}
	node.Limit = value.Set(4)

	s, err := CompileRead(node, t.Context(), false, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT "records"."id" AS "id", COUNT(*) AS "count" FROM "records" WHERE ("records"."id" > $1) GROUP BY "records"."id" HAVING ((COUNT(*) > $2) AND ((SUM(CAST("records"."id" AS numeric)) > $3) OR (NOT ("records"."id" = ANY($4))))) ORDER BY COUNT(*) DESC, "records"."id" ASC LIMIT $5`
	if s.SQLText != want || !reflect.DeepEqual(s.Bindings, []any{int64(0), int64(2), "5", "{1,2}", int64(4)}) {
		t.Fatal(s)
	}
	derived := node
	derived.Having = append(append([]Expression(nil), node.Having...), Comparison{Operand: sum, Operator: IsNotNull})
	derived.Orders = append(append([]OrderNode(nil), node.Orders...), OrderNode{Expression: sum})
	changed, e := CompileRead(derived, t.Context(), false, nil, true)
	if e != nil || !strings.Contains(changed.SQLText, "IS NOT NULL") {
		t.Fatal(changed, e)
	}
	again, e := CompileRead(node, t.Context(), false, nil, true)
	if e != nil || again.SQLText != s.SQLText || !reflect.DeepEqual(again.Bindings, s.Bindings) {
		t.Fatal("derived HAVING changed input", again, e)
	}
}
