package rows_test

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

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
	outer, inner := rows.As[left](m.Query(), "parent"), rows.As[right](m.Query(), "child")
	link := rows.Correlate(outer, inner)
	p := expr.At(rows.OuterScope(link, rows.Scope(outer)), orm.KeyExpr(m.ID))
	c := expr.At(rows.InnerScope(link, rows.Scope(inner)), orm.KeyExpr(m.ID))
	values := rows.SelectCorrelatedScalar(link, expr.AsSelected(expr.Row(c))).Where(expr.Equal(c, p), expr.Ne(c, 2)).Limit(1)
	v := expr.AsSelected(rows.CorrelatedScalarRowQuery(values))
	q := rows.OrderScalar(rows.WhereScalar(rows.SelectScalar(outer, v), expr.Ne(expr.At(rows.Scope(outer), orm.KeyExpr(m.ID)), 8)), expr.AscValue(v))
	s, e := rows.CompileRead(rows.ReadScalar(q))
	if e != nil || !strings.Contains(s.SQL(), `("child"."id" = "parent"."id")`) || !reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(1), int64(8), int64(2), int64(1)}) {
		t.Fatal(s.SQL(), s.Arguments(), e)
	}
	type third struct{}
	grandchild := rows.As[third](m.Query(), "grandchild")
	nested := rows.Correlate(link, grandchild)
	grandparentID := expr.At(rows.OuterScope(nested, rows.OuterScope(link, rows.Scope(outer))), orm.KeyExpr(m.ID))
	parentID := expr.At(rows.OuterScope(nested, rows.InnerScope(link, rows.Scope(inner))), orm.KeyExpr(m.ID))
	childID := expr.At(rows.InnerScope(nested, rows.Scope(grandchild)), orm.KeyExpr(m.ID))
	pred := nested.Where(expr.Equal(childID, parentID), expr.NotEqual(childID, grandparentID)).Exists()
	s, e = rows.CompileRead(rows.ReadScalar(rows.WhereScalar(rows.SelectScalar(outer, expr.AsSelected(expr.Row(expr.At(rows.Scope(outer), orm.KeyExpr(m.ID))))), link.Where(pred).Exists())))
	if e != nil || !strings.Contains(s.SQL(), `("grandchild"."id" <> "parent"."id")`) {
		t.Fatal(s.SQL(), e)
	}
}
func TestCorrelationGroupingAndIsolation(t *testing.T) {
	m := declareMembers()
	a, b := rows.As[left](m.Query(), "parent"), rows.As[right](m.Query(), "child")
	c := rows.Correlate(a, b)
	x, y := expr.At(rows.OuterScope(c, rows.Scope(a)), orm.KeyExpr(m.ID)), expr.At(rows.InnerScope(c, rows.Scope(b)), orm.KeyExpr(m.ID))
	count := rows.SelectCorrelatedScalar(c, expr.AsSelected(expr.CountRow(expr.Row(y)))).Where(expr.Equal(y, x))
	v := expr.AsSelected(rows.CorrelatedScalarRowQuery(count))
	if _, e := rows.CompileRead(rows.ReadScalar(rows.GroupByScalar(rows.SelectScalar(a, v), expr.Group(expr.At(rows.Scope(a), orm.KeyExpr(m.ID)))))); e != nil {
		t.Fatal(e)
	}
	aggregate := expr.Count[rows.Alias[left, member]]()
	exists := c.Where(expr.Equal(y, x)).Exists()
	if _, e := rows.CompileRead(rows.ReadScalar(rows.HavingScalar(rows.SelectScalar(a, v), expr.GtValue(aggregate, 0)))); !errors.Is(e, fault.Invalid) {
		t.Fatal("ungrouped correlated selection", e)
	}
	if _, e := rows.CompileRead(rows.ReadScalar(rows.HavingScalar(rows.SelectScalar(a, expr.AsSelected(aggregate)), expr.Grouped(exists)))); !errors.Is(e, fault.Invalid) {
		t.Fatal("ungrouped correlated HAVING", e)
	}
	if _, e := rows.CompileRead(rows.ReadScalar(rows.WhereScalar(rows.SelectScalar(a, expr.AsSelected(aggregate)), exists))); e != nil {
		t.Fatal("row filter required outer grouping", e)
	}
	if _, e := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(rows.CorrelatedScalarRowQuery(rows.SelectCorrelatedScalar(c, expr.AsSelected(expr.CountRow(expr.Row(x))))))))); !errors.Is(e, fault.Invalid) {
		t.Fatal("outer-only aggregate changed owner", e)
	}
	// Ordinary inner SELECTs must not inherit enclosing correlation visibility.
	node := rows.CorrelatedSourceCore(&c).Source.Node
	node.Selections = []querycore.SelectItem{{Expression: querycore.FieldRef{Table: "parent", Column: "id"}}}
	outer := rows.SourceOf(a).Node
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
	outer := rows.From(m.Query())
	inner := rows.As[right](m.Query(), "child")
	c := rows.Correlate(outer, inner)
	x, y := expr.At(rows.OuterScope(c, rows.ModelScope(m.Query())), orm.KeyExpr(m.ID)), expr.At(rows.InnerScope(c, rows.Scope(inner)), orm.KeyExpr(m.ID))
	p := expr.FoundryPredicate(c.Where(expr.Equal(y, x)).Exists(), sqlowner.Expressions()).Node.(querycore.SubqueryPredicate)
	changed := querycore.Requalify(p, "parent_alias").(querycore.SubqueryPredicate)
	if changed.Query.Correlation.Sources["parent_alias"] == nil || p.Query.Correlation.Sources["members"] == nil {
		t.Fatal("scope mutation")
	}
	alias := rows.As[left](m.Query(), "parent_alias")
	q := rows.SelectScalar(alias, expr.AsSelected(expr.Row(expr.At(rows.Scope(alias), orm.KeyExpr(m.ID)))))
	rows.ScalarSource(&q).Node.Predicates = []querycore.Expression{changed}
	s, e := rows.CompileRead(rows.ReadScalar(q))
	if e != nil || !strings.Contains(s.SQL(), `("child"."id" = "parent_alias"."id")`) {
		t.Fatal(s.SQL(), e)
	}
	wrong := rows.As[left](m.Query(), "wrong")
	link := rows.Correlate(alias, inner)
	if _, e := rows.CompileRead(rows.ReadScalar(rows.WhereScalar(rows.SelectScalar(wrong, expr.AsSelected(expr.Row(expr.At(rows.Scope(wrong), orm.KeyExpr(m.ID))))), link.Exists()))); !errors.Is(e, fault.Invalid) {
		t.Fatal("alias identity mismatch", e)
	}
}
