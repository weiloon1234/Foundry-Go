package querycore

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"strings"
	"testing"
)

func structuralSelect(table string) SelectNode {
	columns := []Column{{Name: "id"}}
	return SelectNode{Source: TableSource{Table: table, Columns: columns}, Selections: ColumnSelections(table, columns)}
}
func TestRelationshipAliasAnalysisBoundsMalformedTrees(t *testing.T) {
	node := structuralSelect("records")
	node.Predicates = make([]Expression, 1)
	node.Predicates[0] = SubqueryPredicate{Query: Subquery{Node: node}}
	if _, err := NamesInSelects(node); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "resource bound") {
		t.Fatal("cyclic alias analysis did not terminate", err)
	}
	wide := structuralSelect("records")
	wide.GroupBy = make([]ValueExpression, MaxExpressionNodes+1)
	if _, err := NamesInSelects(wide); !errors.Is(err, fault.Invalid) {
		t.Fatal("wide alias analysis unbounded", err)
	}
}
func TestCorrelationQualificationBoundsCycles(t *testing.T) {
	p := SubqueryPredicate{Query: Subquery{Node: structuralSelect("child"), Correlation: &ScopeRequirement{Sources: map[string][]Column{"records": {{Name: "id"}}}}}}
	p.Query.Node.Predicates = make([]Expression, 1)
	p.Query.Node.Predicates[0] = p
	changed := RequalifyCorrelation(p.Query, "parent")
	if !errors.Is(changed.Err, fault.Invalid) || !strings.Contains(changed.Err.Error(), "resource bound") {
		t.Fatal("cyclic qualification was not bounded", changed.Err)
	}
	if p.Query.Correlation.Sources["records"] == nil || p.Query.Correlation.Sources["parent"] != nil {
		t.Fatal("failed qualification mutated original scope")
	}
}
func TestCorrelatedJoinConditionCannotSeeLaterSources(t *testing.T) {
	for _, name := range []string{"a", "later"} {
		node := structuralSelect("a")
		col := []Column{{Name: "id"}}
		node.Joins = []JoinNode{
			{Source: TableSource{Table: "b", Columns: col}, Kind: InnerJoin, On: BinaryComparison{Left: FieldRef{Table: "a", Column: "id"}, Right: FieldRef{Table: "b", Column: "id"}, Operator: Equal}},
			{Source: TableSource{Table: "later", Columns: col}, Kind: InnerJoin, On: BinaryComparison{Left: FieldRef{Table: "a", Column: "id"}, Right: FieldRef{Table: "later", Column: "id"}, Operator: Equal}},
		}
		inner := structuralSelect("records")
		inner.Predicates = []Expression{BinaryComparison{Left: FieldRef{Table: "records", Column: "id"}, Right: FieldRef{Table: name, Column: "id"}, Operator: Equal}}
		node.Joins[0].On = Junction{Children: []Expression{node.Joins[0].On, SubqueryPredicate{Query: Subquery{Node: inner, Correlation: &ScopeRequirement{Sources: map[string][]Column{name: col}}}}}}
		var c Compiler
		_, err := c.SelectSQL(node)
		if name == "a" && err != nil {
			t.Fatal("available correlated ON rejected", err)
		}
		if name == "later" && !errors.Is(err, fault.Invalid) {
			t.Fatal("forward correlated ON accepted", err)
		}
	}
}
func TestCTECyclesFailBeforePublishingSQL(t *testing.T) {
	d := &CteNode{Name: "cycle", Columns: []Column{{Name: "id"}}}
	d.Query = SelectNode{Source: TableSource{Cte: d, Columns: d.Columns}, Selections: ColumnSelections("cycle", d.Columns)}
	var c Compiler
	if _, err := c.CompileSelect(d.Query); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "cyclic") {
		t.Fatal("CTE dependency cycle accepted", err)
	}
}
func TestWindowCycleAnalysisIsBounded(t *testing.T) {
	var n WindowNode
	n.Kind = RowNumberWindow
	n.Window.Orders = make([]OrderNode, 1)
	n.Window.Orders[0].Expression = n
	node := structuralSelect("records")
	node.Selections = []SelectItem{{Expression: n}}
	var c Compiler
	if _, err := c.CompileSelect(node); !errors.Is(err, fault.Invalid) {
		t.Fatal("window cycle bypassed bound", err)
	}
}
