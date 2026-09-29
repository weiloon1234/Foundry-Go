package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestSubqueriesShareScopesAndParameterOrder(t *testing.T) {
	id := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	inner := SelectValue(cursorQuery(), id.Value()).Where(id.Gt(2)).OrderBy(id.Asc()).Limit(3)
	scalar := ScalarQuery(cursorQuery(), inner)
	outer := SelectValue(cursorQuery().Where(id.Ne(9)), scalar).Where(id.InQuery(inner)).OrderBy(scalar.Asc()).Limit(4)
	s, err := outer.Compile()
	if err != nil {
		t.Fatal(err)
	}
	wantInner := `SELECT "records"."id" AS "value" FROM "records" WHERE ("records"."id" > `
	if strings.Count(s.SQL(), wantInner) != 3 || !strings.Contains(s.SQL(), `("records"."id" IN (SELECT`) {
		t.Fatal("subqueries did not retain complete inner selects", s.SQL())
	}
	if !reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(3), int64(9), int64(2), int64(3), int64(2), int64(3), int64(4)}) {
		t.Fatal("subquery bindings were duplicated or misplaced", s.Arguments())
	}
	before, _ := inner.Compile()
	_, _ = inner.Where(id.Lt(7)).Limit(1).Compile()
	after, _ := inner.Compile()
	if before.SQL() != after.SQL() || !reflect.DeepEqual(before.Arguments(), after.Arguments()) {
		t.Fatal("derived value query mutated input")
	}
	// Requalification touches only the outer membership column, preserving the
	// inner table reference even when its original name matches the outer one.
	p := id.InQuery(inner).expression
	changed := requalify(p, "outer_record").(subqueryPredicate)
	if changed.operand.(fieldRef).table != "outer_record" || p.(subqueryPredicate).operand.(fieldRef).table != "records" || changed.query.node.source.table != "records" {
		t.Fatal("membership requalification crossed a SELECT boundary")
	}
}

func TestSubqueryValidationAndResourceBounds(t *testing.T) {
	id := NewScalarField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	var absent *ValueQuery[cursorRecord, int64]
	var absentOuter *Query[cursorRecord]
	valid := SelectValue(cursorQuery(), id.Value())
	for name, q := range map[string]ValueQuery[cursorRecord, value.Nullable[int64]]{
		"nil inner":       SelectValue(cursorQuery(), ScalarQuery(cursorQuery(), absent)),
		"zero inner":      SelectValue(cursorQuery(), ScalarQuery(cursorQuery(), ValueQuery[cursorRecord, int64]{})),
		"nil outer":       SelectValue(cursorQuery(), ScalarQuery(absentOuter, valid)),
		"negative window": SelectValue(cursorQuery(), ScalarQuery(cursorQuery(), valid.Limit(-1))),
	} {
		if s, err := q.Compile(); !errors.Is(err, fault.Invalid) || s.SQL() != "" {
			t.Fatal(name, err)
		}
	}
	for name, p := range map[string]Predicate[cursorRecord]{
		"nil IN": id.InQuery(absent), "zero IN": id.InQuery(ValueQuery[cursorRecord, int64]{}),
		"nil EXISTS": ExistsQuery(cursorQuery(), nil), "missing metadata": ExistsQuery(cursorQuery(), For[cursorRecord]("records")),
		"incomplete record": ExistsQuery(cursorQuery(), Project(cursorQuery(), reportDefinition())),
		"eager input":       ExistsQuery(cursorQuery(), cursorQuery().With(testRelation(cursorQuery(), cursorQuery()))),
	} {
		if _, err := cursorQuery().Where(p).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal(name, err)
		}
	}
	if _, err := SelectValue(cursorQuery(), ScalarQuery(cursorQuery(), SelectValue(cursorQuery(), Nullable(id.Value())))).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("nested nullable scalar was accepted", err)
	}
	for i := 0; i < MaxExpressionDepth; i++ {
		valid = SelectValue(cursorQuery().Where(id.InQuery(valid)), id.Value())
	}
	if _, err := valid.Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("deep predicate subquery accepted", err)
	}
	// A privately malformed scalar cycle must terminate through the same bound.
	node := cursorQuery().modelSelect()
	nested := &selectNode{}
	node.selections = []selectItem{{expression: scalarSubquery{query: subquery{node: selectNode{
		source:     tableSource{alias: "loop", query: nested, columns: []Column{{Name: "id"}}},
		selections: []selectItem{{expression: fieldRef{"loop", "id"}}},
	}}}}}
	*nested = node
	var c compiler
	if _, err := c.selectSQL(node); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "resource bound") {
		t.Fatal("scalar/derived cycle accepted", err)
	}
}

func TestSubqueriesShareTheParameterBudget(t *testing.T) {
	id := NewScalarField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	base := cursorQuery()
	inner := SelectValue(base.Where(wideScoped(base.Scope(), MaxParameters/2+1)), id.Value())
	outer := base.Where(wideScoped(base.Scope(), MaxParameters/2+1), id.InQuery(inner))
	if s, err := outer.Compile(); !errors.Is(err, fault.Invalid) || s.SQL() != "" || len(s.Arguments()) != 0 {
		t.Fatal("nested statements bypassed the shared parameter bound", err)
	}
}

func TestSubqueryScopesDoNotAccidentallyCorrelate(t *testing.T) {
	outer := As[firstAlias](cursorQuery(), "outer_record")
	inner := cursorQuery().modelSelect()
	inner.predicates = []expression{binaryComparison{left: fieldRef{"records", "id"}, right: fieldRef{"outer_record", "id"}, operator: equal}}
	q := SelectValue(outer, Expression[Alias[firstAlias, cursorRecord], value.Nullable[int64]]{node: scalarSubquery{query: subquery{node: inner}}})
	if _, err := q.Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("inner query silently correlated to an outer alias", err)
	}
	// Independent SELECT scopes may reuse names and retain outer state afterward.
	a := As[firstAlias](cursorQuery(), "records")
	id := scopedID(a.Scope())
	s, err := SelectValue(a, ScalarQuery(a, SelectValue(cursorQuery(), NewScalarField[cursorRecord, int64]("records", "id", codec.Signed[int64]()).Value()).Limit(1))).Where(id.Eq(4)).Compile()
	if err != nil || !strings.Contains(s.SQL(), `WHERE ("records"."id" = $2)`) {
		t.Fatal("inner scope leaked into outer query", s.SQL(), err)
	}
}

func TestExistsKeepsAggregateAndWindowSemantics(t *testing.T) {
	inner := SelectValue(cursorQuery(), Count[cursorRecord]().Value()).Having(Count[cursorRecord]().Eq(0)).Limit(0)
	s, err := cursorQuery().Where(ExistsQuery(cursorQuery(), inner).Not()).Compile()
	if err != nil || !strings.Contains(s.SQL(), `NOT EXISTS(SELECT COUNT(*) AS "value" FROM "records" HAVING (COUNT(*) = $1) LIMIT $2)`) {
		t.Fatal("EXISTS changed aggregate/window semantics", s.SQL(), err)
	}
}
