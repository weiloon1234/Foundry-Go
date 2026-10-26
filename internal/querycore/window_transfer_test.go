package querycore

import (
	"database/sql/driver"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
	"strings"
	"testing"
)

func TestWindowAggregatesUseOrdinaryAggregateCompiler(t *testing.T) {
	f := FieldRef{Table: "records", Column: "id"}
	w := WindowSpec{Orders: []OrderNode{{Expression: f}}}
	for _, test := range []struct {
		a   AggregateNode
		sql string
	}{
		{AggregateNode{Kind: CountAll}, "COUNT(*)"}, {AggregateNode{Kind: CountValues, Field: f}, `COUNT("records"."id")`}, {AggregateNode{Kind: SumValues, Field: f, Cast: ExactNumberCast}, `SUM(CAST("records"."id" AS numeric))`}, {AggregateNode{Kind: AverageValues, Field: f, Cast: ExactNumberCast}, `AVG(CAST("records"."id" AS numeric))`}, {AggregateNode{Kind: MinimumValue, Field: f}, `MIN("records"."id")`}, {AggregateNode{Kind: MaximumValue, Field: f}, `MAX("records"."id")`},
	} {
		n := structuralSelect("records")
		n.Selections = append(n.Selections, SelectItem{Expression: WindowNode{Kind: AggregateWindow, Aggregate: test.a, Window: w}})
		s, e := CompileRead(n, t.Context(), false, nil, true)
		if e != nil || !strings.Contains(s.SQLText, test.sql+` OVER (ORDER BY "records"."id" ASC)`) || strings.Contains(s.SQLText, "GROUP BY") {
			t.Fatal(test.sql, s.SQLText, e)
		}
	}
	n := structuralSelect("records")
	n.Selections = []SelectItem{{Expression: WindowNode{Kind: AggregateWindow, Aggregate: AggregateNode{Kind: ExistsValues}, Window: w}}}
	s, e := CompileRead(n, t.Context(), false, nil, true)
	if e != nil || !strings.Contains(s.SQLText, `(COUNT(*) OVER (ORDER BY "records"."id" ASC) > 0)`) {
		t.Fatal(s.SQLText, e)
	}
	count := AggregateNode{Kind: CountAll}
	n.Selections = []SelectItem{{Expression: WindowNode{Kind: RowNumberWindow, Window: WindowSpec{Orders: []OrderNode{{Expression: count, Descending: true}}}}}}
	if _, e := CompileRead(n, t.Context(), false, nil, true); e != nil {
		t.Fatal(e)
	}
	bad := n.Selections[0].Expression.(WindowNode)
	bad.Window.Orders = append(bad.Window.Orders, OrderNode{Expression: f})
	n.Selections[0].Expression = bad
	if _, e := CompileRead(n, t.Context(), false, nil, true); !errors.Is(e, fault.Invalid) {
		t.Fatal("ungrouped field", e)
	}
	n.Selections[0].Expression = WindowNode{Kind: LagWindow, Input: count, N: RetainOffset(1)}
	if _, e := CompileRead(n, t.Context(), false, nil, true); e != nil {
		t.Fatal("aggregate input", e)
	}
}
func TestWindowPrivateValidationAndBudgets(t *testing.T) {
	f := FieldRef{Table: "records", Column: "id"}
	w := WindowSpec{Orders: []OrderNode{{Expression: f}}}
	row := WindowNode{Kind: RowNumberWindow, Window: w}
	for name, v := range map[string]ValueExpression{"zero kind": WindowNode{}, "zero nth": WindowNode{Kind: NthValueWindow, Input: f, N: RetainOffset(0), Window: w}, "nested input": WindowNode{Kind: LagWindow, Input: row, N: RetainOffset(1), Window: w}, "zero value": WindowNode{Kind: FirstValueWindow, Window: w}} {
		n := structuralSelect("records")
		n.Selections = []SelectItem{{Expression: v}}
		if _, e := CompileRead(n, t.Context(), false, nil, true); !errors.Is(e, fault.Invalid) {
			t.Fatal(name, e)
		}
	}
	n := structuralSelect("records")
	n.Selections = []SelectItem{{Expression: AggregateNode{Kind: CountAll}}}
	n.Having = []Expression{Comparison{Operand: row, Operator: Equal, Values: RetainComparisonValues([]any{int64(1)}), Kind: codec.TypeInteger, Bind: func(x any) (driver.Value, error) { return x, nil }}}
	if _, e := CompileRead(n, t.Context(), false, nil, true); !errors.Is(e, fault.Invalid) {
		t.Fatal("window HAVING", e)
	}
	n.Having = nil
	n.Selections = []SelectItem{{Expression: WindowNode{Kind: FirstValueWindow, Input: f, Window: WindowSpec{Orders: w.Orders, Frame: &WindowFrame{Kind: RowsFrame, Start: FrameBound{Kind: PrecedingBound, Offset: RetainOffset(7)}, End: FrameBound{Kind: PrecedingBound, Offset: RetainOffset(8)}}}}}}
	if _, e := CompileRead(n, t.Context(), false, nil, true); e != nil {
		t.Fatal("legal empty frame", e)
	}
	n.Selections = []SelectItem{{Expression: f}}
	values := make([]any, MaxParameters)
	for i := range values {
		values[i] = float64(i)
	}
	n.Predicates = []Expression{Comparison{Operand: f, Operator: In, Kind: codec.TypeFloat, Values: RetainComparisonValues(values), Bind: func(x any) (driver.Value, error) { return x, nil }}}
	if _, e := CompileRead(n, t.Context(), false, nil, true); e != nil {
		t.Fatal("parameter control", e)
	}
	n.Selections[0].Expression = WindowNode{Kind: LagWindow, Input: f, N: RetainOffset(1), HasFallback: true, Fallback: RetainLiteral(int64(2))}
	if _, e := CompileRead(n, t.Context(), false, nil, true); !errors.Is(e, fault.Invalid) {
		t.Fatal("window parameter bound", e)
	}
	cycle := WindowNode{Kind: RowNumberWindow, Window: WindowSpec{Orders: make([]OrderNode, 1)}}
	cycle.Window.Orders[0].Expression = cycle
	n.Predicates = nil
	n.Selections[0].Expression = cycle
	if _, e := CompileRead(n, t.Context(), false, nil, true); !errors.Is(e, fault.Invalid) {
		t.Fatal("window cycle", e)
	}
}
func TestWindowSubqueriesAndCorrelationKeepSelectLevels(t *testing.T) {
	f := FieldRef{Table: "records", Column: "id"}
	inner := structuralSelect("records")
	inner.Selections = []SelectItem{{Expression: WindowNode{Kind: RowNumberWindow, Window: WindowSpec{Orders: []OrderNode{{Expression: f}}}}}}
	inner.Limit = value.Set(1)
	outer := structuralSelect("records")
	outer.Selections = []SelectItem{{Expression: WindowNode{Kind: LagWindow, Input: ScalarSubquery{Query: Subquery{Node: inner}}, N: RetainOffset(1)}}}
	s, e := CompileRead(outer, t.Context(), false, nil, true)
	if e != nil || strings.Count(s.SQLText, " OVER (") != 2 {
		t.Fatal(s.SQLText, e)
	}
	cte := CteNode{Name: "numbered", Query: structuralSelect("records"), Columns: []Column{{Name: "id"}}}
	inner.Source = TableSource{Cte: &cte, Alias: "a", Columns: cte.Columns}
	inner.Selections = []SelectItem{{Expression: FieldRef{Table: "a", Column: "id"}}}
	outer.Selections[0].Expression = WindowNode{Kind: RowNumberWindow, Window: WindowSpec{Orders: []OrderNode{{Expression: ScalarSubquery{Query: Subquery{Node: inner}}}}}}
	s, e = CompileRead(outer, t.Context(), false, nil, true)
	if e != nil || !strings.HasPrefix(s.SQLText, `WITH "numbered"`) {
		t.Fatal(s.SQLText, e)
	}
	child := structuralSelect("child")
	cf := FieldRef{Table: "child", Column: "id"}
	child.Selections = []SelectItem{{Expression: WindowNode{Kind: AggregateWindow, Aggregate: AggregateNode{Kind: CountValues, Field: f}, Window: WindowSpec{Partitions: []ValueExpression{f}, Orders: []OrderNode{{Expression: cf}}}}}}
	child.Predicates = []Expression{BinaryComparison{Left: cf, Right: f, Operator: Equal}}
	child.Limit = value.Set(1)
	sub := Subquery{Node: child, Correlation: &ScopeRequirement{Sources: map[string][]Column{"records": {{Name: "id"}}}}}
	changed := RequalifyCorrelation(sub, "qualified")
	n := changed.Node.Selections[0].Expression.(WindowNode)
	original := sub.Node.Selections[0].Expression.(WindowNode)
	if n.Aggregate.Field.Table != "qualified" || !reflect.DeepEqual(n.Window.Partitions[0], FieldRef{Table: "qualified", Column: "id"}) || !reflect.DeepEqual(original.Window.Partitions[0], f) {
		t.Fatal("window correlation not copied")
	}
	outer.Selections[0].Expression = ScalarSubquery{Query: sub}
	if _, e := CompileRead(outer, t.Context(), false, nil, true); e != nil {
		t.Fatal("outer field aggregate owner", e)
	}
}
