package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestRelationPredicatesNestWithoutAliasCapture(t *testing.T) {
	id := NewScalarField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	base := testRelation(cursorQuery(), cursorQuery())
	r := base.Where(id.Eq(7))
	for range 5 {
		r = base.Where(r.Exists())
	}
	q := cursorQuery().WhereHas(r).WhereDoesntHave(base.Where(id.Eq(9)))
	s, err := q.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(s.SQL(), "EXISTS(") != 7 || !strings.Contains(s.SQL(), `AS "foundry_related_6"`) || !reflect.DeepEqual(s.Arguments(), []any{int64(7), int64(9)}) {
		t.Fatal("nested self filters lost names, conditions or parameter order", s.SQL(), s.Arguments())
	}
	r = r.Where(id.Eq(11))
	again, err := q.Compile()
	if err != nil || s.SQL() != again.SQL() || !reflect.DeepEqual(s.Arguments(), again.Arguments()) {
		t.Fatal("relation predicate retained a mutable descriptor", err)
	}
	// Explicit inner names must also be preserved when outer relationship
	// compilation selects automatic aliases.
	inner := As[firstAlias](cursorQuery(), "foundry_related")
	link := Correlate(cursorQuery(), inner)
	a, b := scopedID(OuterScope(link, cursorQuery().Scope())), scopedID(InnerScope(link, inner.Scope()))
	manual := link.Where(b.EqColumn(a)).Exists()
	for _, r := range []ExistenceRelation[cursorRecord]{base.Where(manual), testThrough().Where(manual).WherePivot(base.Exists())} {
		if _, err := cursorQuery().WhereHas(r).Compile(); err != nil {
			t.Fatal("relationship aliases captured a declared nested source", err)
		}
	}
}

func TestRelationPredicatesUseOnlyFilterScopes(t *testing.T) {
	base := testRelation(cursorQuery(), cursorQuery())
	id := NewScalarField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	r := base.Where(id.Eq(2)).OrderBy(id.Desc()).With(base)
	s, err := cursorQuery().WhereHas(r).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s.SQL(), "ORDER BY") || strings.Count(s.SQL(), "SELECT") != 2 || !reflect.DeepEqual(s.Arguments(), []any{int64(2)}) {
		t.Fatal("existence used eager/order clauses", s.SQL())
	}
	if len(r.spec.target.orders) != 1 || len(r.spec.target.relations) != 1 {
		t.Fatal("existence stripped the reusable descriptor")
	}
	var missing *OneRelation[cursorRecord, cursorRecord]
	window := base
	window.spec.target = window.spec.target.Limit(1)
	for _, q := range []Query[cursorRecord]{
		cursorQuery().WhereHas(missing), cursorQuery().WhereDoesntHave(nil),
		cursorQuery().WhereHas(OneRelation[cursorRecord, cursorRecord]{}),
		cursorQuery().WhereHas(ThroughRelation[cursorRecord, cursorRecord, cursorRecord]{}),
		cursorQuery().WhereHas(window),
		cursorQuery().WhereHas(base.Where(NewScalarField[cursorRecord, int64]("records", "missing", codec.Signed[int64]()).Eq(1))),
	} {
		if s, err := q.Compile(); !errors.Is(err, fault.Invalid) || s.SQL() != "" {
			t.Fatal("invalid relationship predicate accepted", err)
		}
	}
}

func TestRelationshipAliasAnalysisBoundsMalformedTrees(t *testing.T) {
	base := testRelation(cursorQuery(), cursorQuery())
	node := cursorQuery().modelSelect()
	node.predicates = make([]expression, 1)
	node.predicates[0] = subqueryPredicate{query: subquery{node: node}}
	base.spec.target.predicates = node.predicates
	if _, err := cursorQuery().WhereHas(base).Compile(); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "resource bound") {
		t.Fatal("cyclic alias analysis did not terminate", err)
	}
	wide := cursorQuery().modelSelect()
	wide.groupBy = make([]valueExpression, MaxExpressionNodes+1)
	if _, err := namesInSelects(wide); !errors.Is(err, fault.Invalid) {
		t.Fatal("wide alias analysis was not bounded", err)
	}
}
