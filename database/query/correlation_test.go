package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestCorrelationSQLAndNestedOwnership(t *testing.T) {
	outer := As[firstAlias](cursorQuery(), "parent")
	inner := As[secondAlias](cursorQuery(), "child")
	link := Correlate(outer, inner)
	p := scopedID(OuterScope(link, outer.Scope()))
	c := scopedID(InnerScope(link, inner.Scope()))
	values := SelectCorrelatedValue(link, Value(c)).Where(c.EqColumn(p), c.Ne(2)).Limit(1)
	expression := CorrelatedScalarQuery(values)
	s, err := SelectValue(outer, expression).Where(scopedID(outer.Scope()).Ne(8)).OrderBy(expression.Asc()).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.SQL(), `("child"."id" = "parent"."id")`) || !reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(1), int64(8), int64(2), int64(1)}) {
		t.Fatal("correlated SQL/parameter order changed", s.SQL(), s.Arguments())
	}
	third := As[thirdAlias](cursorQuery(), "grandchild")
	nested := Correlate(link, third)
	grandparent := scopedID(OuterScope(nested, OuterScope(link, outer.Scope())))
	parent := scopedID(OuterScope(nested, InnerScope(link, inner.Scope())))
	child := scopedID(InnerScope(nested, third.Scope()))
	predicate := nested.Where(child.EqColumn(parent), child.NeColumn(grandparent)).Exists()
	if s, err := SelectValue(outer, Value(scopedID(outer.Scope()))).Where(link.Where(predicate).Exists()).Compile(); err != nil || !strings.Contains(s.SQL(), `("grandchild"."id" <> "parent"."id")`) {
		t.Fatal("nested correlation lost enclosing scopes", s.SQL(), err)
	}
}

func TestCorrelationGroupingAndIsolation(t *testing.T) {
	a, b := As[firstAlias](cursorQuery(), "parent"), As[secondAlias](cursorQuery(), "child")
	c := Correlate(a, b)
	x, y := scopedID(OuterScope(c, a.Scope())), scopedID(InnerScope(c, b.Scope()))
	count := SelectCorrelatedValue(c, CountField(y).Value()).Where(y.EqColumn(x))
	expression := CorrelatedScalarQuery(count)
	if _, err := SelectValue(a, expression).GroupBy(scopedID(a.Scope()).Group()).Compile(); err != nil {
		t.Fatal(err)
	}
	if _, err := SelectValue(a, expression).Having(Count[Alias[firstAlias, cursorRecord]]().Gt(0)).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("ungrouped outer SELECT reference accepted", err)
	}
	if _, err := SelectValue(a, Count[Alias[firstAlias, cursorRecord]]().Value()).Having(Grouped(c.Where(y.EqColumn(x)).Exists())).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("ungrouped outer HAVING reference accepted", err)
	}
	// The identical predicate is valid before the outer grouping step.
	if _, err := SelectValue(a, Count[Alias[firstAlias, cursorRecord]]().Value()).Where(c.Where(y.EqColumn(x)).Exists()).Compile(); err != nil {
		t.Fatal("row filter incorrectly requires grouped outer columns", err)
	}
	if _, err := SelectValue(a, CorrelatedScalarQuery(SelectCorrelatedValue(c, CountField(x).Value()))).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("outer-only aggregate silently changed SQL ownership", err)
	}
	// Ordinary nested SELECTs remain isolated, including inside a correlation.
	bad := ValueQuery[Correlation[Alias[firstAlias, cursorRecord], Alias[secondAlias, cursorRecord]], int64]{query: SelectValue(c.source, Value(x)).query}
	isolated := ExistsQuery(c.source, bad)
	if _, err := SelectValue(a, Value(scopedID(a.Scope()))).Where(c.Where(isolated).Exists()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("uncorrelated SELECT inherited parent visibility", err)
	}
}

func TestCorrelationInvalidBoundariesAndRequalification(t *testing.T) {
	outer := cursorQuery()
	inner := As[secondAlias](cursorQuery(), "child")
	c := Correlate(outer, inner)
	x, y := scopedID(OuterScope(c, outer.Scope())), scopedID(InnerScope(c, inner.Scope()))
	p := c.Where(y.EqColumn(x)).Exists()
	qualified := requalify(p.expression, "parent_alias").(subqueryPredicate)
	if qualified.Query.Correlation.Sources["parent_alias"] == nil || p.expression.(subqueryPredicate).Query.Correlation.Sources["records"] == nil {
		t.Fatal("qualification mutated original scope")
	}
	alias := As[firstAlias](cursorQuery(), "parent_alias")
	project := SelectValue(alias, Value(scopedID(alias.Scope())))
	project.query.source.node.Predicates = []expression{qualified}
	s, err := project.Compile()
	if err != nil || !strings.Contains(s.SQL(), `("child"."id" = "parent_alias"."id")`) {
		t.Fatal("qualified correlation still points to old parent", s.SQL(), err)
	}
	for name, predicate := range map[string]Predicate[cursorRecord]{
		"shadow": Correlate(outer, cursorQuery()).Exists(),
		"nil":    Correlate[cursorRecord, cursorRecord](nil, cursorQuery()).Exists(),
		"zero":   CorrelatedSource[cursorRecord, cursorRecord]{}.Exists(),
	} {
		if _, err := outer.Where(predicate).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal(name, err)
		}
	}
	wrong := As[firstAlias](cursorQuery(), "wrong")
	link := Correlate(alias, inner)
	if _, err := SelectValue(wrong, Value(scopedID(wrong.Scope()))).Where(link.Exists()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("runtime outer alias identity mismatch accepted", err)
	}
}

func TestNestedCorrelationRetainsGrandparentGrouping(t *testing.T) {
	a, b, child := As[firstAlias](cursorQuery(), "a"), As[secondAlias](cursorQuery(), "b"), As[thirdAlias](cursorQuery(), "child")
	parent := Correlate(a, b)
	nested := Correlate(parent, child)
	grandparentID := scopedID(OuterScope(nested, OuterScope(parent, a.Scope())))
	childID := scopedID(InnerScope(nested, child.Scope()))
	exists := parent.Where(nested.Where(childID.EqColumn(grandparentID)).Exists()).Exists()
	grouped := SelectValue(a, Count[Alias[firstAlias, cursorRecord]]().Value()).Having(Grouped(exists))
	if _, err := grouped.Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("nested correlation bypassed ungrouped grandparent validation", err)
	}
	if _, err := grouped.GroupBy(scopedID(a.Scope()).Group()).Compile(); err != nil {
		t.Fatal("grouped grandparent could not be referenced through parent", err)
	}
}

func TestCorrelatedJoinConditionCannotSeeLaterSources(t *testing.T) {
	a, b, later := As[firstAlias](cursorQuery(), "a"), As[secondAlias](cursorQuery(), "b"), As[thirdAlias](cursorQuery(), "later")
	first := InnerJoin(a, b, On(scopedID(a.Scope()), scopedID(b.Scope())))
	joined := InnerJoin(first, later, On(scopedID(LeftScope(first, a.Scope())), scopedID(later.Scope())))
	for _, test := range []struct {
		name  string
		scope scopeRequirement
		valid bool
	}{
		{"a", a.scopeSource().scopeRequirement, true},
		{"later", later.scopeSource().scopeRequirement, false},
	} {
		node := joined.input.node
		node.Joins = append([]joinNode(nil), node.Joins...)
		inner := cursorQuery().modelSelect()
		inner.Predicates = []expression{binaryComparison{Left: fieldRef{Table: "records", Column: "id"}, Right: fieldRef{Table: test.name, Column: "id"}, Operator: equal}}
		node.Joins[0].On = junction{Children: []expression{node.Joins[0].On,
			subqueryPredicate{Query: subquery{Node: inner, Correlation: &test.scope}},
		}}
		var c compiler
		_, err := c.SelectSQL(node)
		if test.valid && err != nil {
			t.Fatal("correlated ON rejected an available source", err)
		}
		if !test.valid && !errors.Is(err, fault.Invalid) {
			t.Fatal("correlated ON accepted a forward source reference", err)
		}
	}
}

func TestCorrelationQualificationBoundsCycles(t *testing.T) {
	outer := cursorQuery()
	child := As[secondAlias](outer, "child")
	c := Correlate(outer, child)
	p := c.Exists().expression.(subqueryPredicate)
	// Shared slice storage deliberately creates a malformed private AST cycle.
	p.Query.Node.Predicates = make([]expression, 1)
	p.Query.Node.Predicates[0] = p
	qualified := requalifyCorrelation(p.Query, "parent")
	if !errors.Is(qualified.Err, fault.Invalid) || !strings.Contains(qualified.Err.Error(), "resource bound") {
		t.Fatal("cyclic qualification was not bounded", qualified.Err)
	}
	if p.Query.Correlation.Sources["records"] == nil || p.Query.Correlation.Sources["parent"] != nil {
		t.Fatal("failed qualification mutated the original scope")
	}
}
