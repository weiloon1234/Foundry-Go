package rows

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"reflect"
	"strings"
	"testing"
)

func TestCorrelationSQLAndNestedOwnership(t *testing.T) {
	m := declareMembers()
	outer, inner := As[left](m.Query(), "parent"), As[right](m.Query(), "child")
	link := Correlate(outer, inner)
	p := expr.At(OuterScope(link, Scope(outer)), orm.KeyExpr(m.ID))
	c := expr.At(InnerScope(link, Scope(inner)), orm.KeyExpr(m.ID))
	values := SelectCorrelatedScalar(link, expr.Row(c).Value()).Where(expr.Equal(c, p), c.Ne(2)).Limit(1)
	v := CorrelatedScalarRowQuery(values).Value()
	q := OrderScalar(WhereScalar(SelectScalar(outer, v), expr.At(Scope(outer), orm.KeyExpr(m.ID)).Ne(8)), v.Asc())
	s, e := CompileRead(ReadScalar(q))
	if e != nil || !strings.Contains(s.SQL(), `("child"."id" = "parent"."id")`) || !reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(1), int64(8), int64(2), int64(1)}) {
		t.Fatal(s.SQL(), s.Arguments(), e)
	}
	type third struct{}
	grandchild := As[third](m.Query(), "grandchild")
	nested := Correlate(link, grandchild)
	grandparentID := expr.At(OuterScope(nested, OuterScope(link, Scope(outer))), orm.KeyExpr(m.ID))
	parentID := expr.At(OuterScope(nested, InnerScope(link, Scope(inner))), orm.KeyExpr(m.ID))
	childID := expr.At(InnerScope(nested, Scope(grandchild)), orm.KeyExpr(m.ID))
	pred := nested.Where(expr.Equal(childID, parentID), expr.NotEqual(childID, grandparentID)).Exists()
	s, e = CompileRead(ReadScalar(WhereScalar(SelectScalar(outer, expr.Row(expr.At(Scope(outer), orm.KeyExpr(m.ID))).Value()), link.Where(pred).Exists())))
	if e != nil || !strings.Contains(s.SQL(), `("grandchild"."id" <> "parent"."id")`) {
		t.Fatal(s.SQL(), e)
	}
}
func TestCorrelationGroupingAndIsolation(t *testing.T) {
	m := declareMembers()
	a, b := As[left](m.Query(), "parent"), As[right](m.Query(), "child")
	c := Correlate(a, b)
	x, y := expr.At(OuterScope(c, Scope(a)), orm.KeyExpr(m.ID)), expr.At(InnerScope(c, Scope(b)), orm.KeyExpr(m.ID))
	count := SelectCorrelatedScalar(c, expr.CountRow(expr.Row(y)).Value()).Where(expr.Equal(y, x))
	v := CorrelatedScalarRowQuery(count).Value()
	if _, e := CompileRead(ReadScalar(GroupByScalar(SelectScalar(a, v), expr.Group(expr.At(Scope(a), orm.KeyExpr(m.ID)))))); e != nil {
		t.Fatal(e)
	}
	aggregate := expr.Count[Alias[left, member]]()
	exists := c.Where(expr.Equal(y, x)).Exists()
	if _, e := CompileRead(ReadScalar(HavingScalar(SelectScalar(a, v), aggregate.Gt(0)))); !errors.Is(e, fault.Invalid) {
		t.Fatal("ungrouped correlated selection", e)
	}
	if _, e := CompileRead(ReadScalar(HavingScalar(SelectScalar(a, aggregate.Value()), expr.Grouped(exists)))); !errors.Is(e, fault.Invalid) {
		t.Fatal("ungrouped correlated HAVING", e)
	}
	if _, e := CompileRead(ReadScalar(WhereScalar(SelectScalar(a, aggregate.Value()), exists))); e != nil {
		t.Fatal("row filter required outer grouping", e)
	}
	if _, e := CompileRead(ReadScalar(SelectScalar(a, CorrelatedScalarRowQuery(SelectCorrelatedScalar(c, expr.CountRow(expr.Row(x)).Value())).Value()))); !errors.Is(e, fault.Invalid) {
		t.Fatal("outer-only aggregate changed owner", e)
	}
	// Ordinary inner SELECTs must not inherit enclosing correlation visibility.
	node := c.core.Source.Node
	node.Selections = []querycore.SelectItem{{Expression: querycore.FieldRef{Table: "parent", Column: "id"}}}
	outer := a.source.Node
	outer.Selections = []querycore.SelectItem{{Expression: querycore.FieldRef{Table: "parent", Column: "id"}}}
	correlated := expr.FoundryPredicate(c.Exists(), sqlowner.Expressions()).Node.(querycore.SubqueryPredicate)
	correlated.Query.Node.Predicates = []querycore.Expression{querycore.SubqueryPredicate{Query: querycore.Subquery{Node: node}}}
	outer.Predicates = []querycore.Expression{correlated}
	if _, e := querycore.CompileRead(outer, t.Context(), false, nil, true); !errors.Is(e, fault.Invalid) {
		t.Fatal("uncorrelated select inherited parent visibility", e)
	}
}
func TestCorrelationInvalidBoundariesAndRequalification(t *testing.T) {
	m := declareMembers()
	outer := From(m.Query())
	inner := As[right](m.Query(), "child")
	c := Correlate(outer, inner)
	x, y := expr.At(OuterScope(c, ModelScope(m.Query())), orm.KeyExpr(m.ID)), expr.At(InnerScope(c, Scope(inner)), orm.KeyExpr(m.ID))
	p := expr.FoundryPredicate(c.Where(expr.Equal(y, x)).Exists(), sqlowner.Expressions()).Node.(querycore.SubqueryPredicate)
	changed := querycore.Requalify(p, "parent_alias").(querycore.SubqueryPredicate)
	if changed.Query.Correlation.Sources["parent_alias"] == nil || p.Query.Correlation.Sources["members"] == nil {
		t.Fatal("scope mutation")
	}
	alias := As[left](m.Query(), "parent_alias")
	q := SelectScalar(alias, expr.Row(expr.At(Scope(alias), orm.KeyExpr(m.ID))).Value())
	q.source.Node.Predicates = []querycore.Expression{changed}
	s, e := CompileRead(ReadScalar(q))
	if e != nil || !strings.Contains(s.SQL(), `("child"."id" = "parent_alias"."id")`) {
		t.Fatal(s.SQL(), e)
	}
	wrong := As[left](m.Query(), "wrong")
	link := Correlate(alias, inner)
	if _, e := CompileRead(ReadScalar(WhereScalar(SelectScalar(wrong, expr.Row(expr.At(Scope(wrong), orm.KeyExpr(m.ID))).Value()), link.Exists()))); !errors.Is(e, fault.Invalid) {
		t.Fatal("alias identity mismatch", e)
	}
}
