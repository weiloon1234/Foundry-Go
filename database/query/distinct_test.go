package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestDistinctKeepsSelectedRowsAndParameters(t *testing.T) {
	base := cursorQuery()
	id := scopedID(base.Scope())
	q := base.Where(id.Ne(7)).Distinct().OrderBy(id.Desc()).Limit(2).Offset(1)
	s, err := q.Compile()
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT DISTINCT "records"."id", "records"."rank" FROM "records" WHERE ("records"."id" <> $1) ORDER BY 1 DESC LIMIT $2 OFFSET $3`
	if s.SQL() != want || !reflect.DeepEqual(s.Arguments(), []any{int64(7), int64(2), int64(1)}) {
		t.Fatal("distinct changed selection or window", s.SQL(), s.Arguments())
	}
	_ = q.DistinctOn(id.Group())
	again, _ := q.Compile()
	if again.SQL() != s.SQL() {
		t.Fatal("derived distinct mutated original")
	}
	if original, err := base.Compile(); err != nil || strings.Contains(original.SQL(), "DISTINCT") {
		t.Fatal("base mutated", err)
	}
	combined := q.UnionAll(base)
	if s, err := combined.Distinct().Compile(); err != nil || strings.Count(s.SQL(), "SELECT DISTINCT ") != 2 {
		t.Fatal("set lost distinct", err)
	}
	values := SelectValue(base, id.Value())
	if s, err := values.UnionAll(values).Distinct().Compile(); err != nil || !strings.HasPrefix(s.SQL(), "SELECT DISTINCT ") {
		t.Fatal("value set lost distinct", err)
	}
}

func TestDistinctOrderingReusesSelectedScalarBindings(t *testing.T) {
	base := cursorQuery()
	id := scopedID(base.Scope())
	inner := SelectValue(base.Where(id.Ne(9)), id.Value()).Limit(1)
	scalar := ScalarQuery(base, inner)
	q := SelectValue(base.Where(id.Eq(7)), scalar).Distinct().OrderBy(scalar.Desc()).Limit(2)
	s, err := q.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(s.SQL(), `SELECT "records"."id" AS "value"`) != 1 || !strings.Contains(s.SQL(), "ORDER BY 1 DESC") || !reflect.DeepEqual(s.Arguments(), []any{int64(9), int64(1), int64(7), int64(2)}) {
		t.Fatal("DISTINCT ordered by a separately bound scalar", s.SQL(), s.Arguments())
	}
	wrong := ScalarQuery(base, SelectValue(base.Where(id.Ne(8)), id.Value()).Limit(1))
	if _, err := SelectValue(base, scalar).Distinct().OrderBy(wrong.Asc()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("different scalar binding matched selection", err)
	}
	// A parameterized CTE precedes a parameter-free selection: nil and empty
	// argument ranges must match, and ordinal references remain local.
	a := As[firstAlias](CTE("filtered", base.Where(id.Ne(3))), "a")
	aid := scopedID(a.Scope())
	if _, err := SelectRecord(a, a.Scope()).Distinct().OrderBy(aid.Asc()).Compile(); err != nil {
		t.Fatal("CTE arguments changed distinct ordering", err)
	}
	// Multiple selected scalars have different absolute placeholder offsets.
	node, _ := q.query.selectNode()
	node.selections = append(node.selections, selectItem{expression: wrong.node, alias: "other"})
	node.orders = []orderNode{{expression: wrong.node}}
	var c compiler
	if sql, err := c.compileSelect(node); err != nil || !strings.Contains(sql, "ORDER BY 2 ASC") {
		t.Fatal("relative placeholders did not match", sql, err)
	}
}

func TestDistinctOnKeysOrderingAndReplacement(t *testing.T) {
	base := cursorQuery()
	id := scopedID(base.Scope())
	rank := NewNullableField[cursorRecord, int64]("records", "rank", codec.Signed[int64]())
	q := base.DistinctOn(rank.Group()).OrderBy(rank.Asc(), id.Desc())
	s, err := q.Compile()
	if err != nil || !strings.HasPrefix(s.SQL(), `SELECT DISTINCT ON ("records"."rank") "records"."id", "records"."rank"`) || !strings.Contains(s.SQL(), `ORDER BY "records"."rank" ASC, "records"."id" DESC`) {
		t.Fatal(s.SQL(), err)
	}
	if s, err := q.Distinct().Compile(); err != nil || strings.Contains(s.SQL(), "DISTINCT ON") {
		t.Fatal("Distinct did not replace On", err)
	}
	keys := []Group[cursorRecord]{id.Group(), rank.Group()}
	permuted := base.DistinctOn(keys...).OrderBy(rank.Asc(), id.Desc())
	keys[0] = Group[cursorRecord]{}
	if _, err := permuted.Compile(); err != nil {
		t.Fatal("keys were not copied or permutation rejected", err)
	}
	if _, err := base.DistinctOn(id.Group(), rank.Group()).OrderBy(id.Asc()).Compile(); err != nil {
		t.Fatal("shortened prefix rejected", err)
	}
	if _, err := base.DistinctOn(rank.Group()).Compile(); err != nil {
		t.Fatal("unordered selection rejected", err)
	}
}

func TestDistinctRejectsInvalidSelectionsBeforeExecution(t *testing.T) {
	base := cursorQuery()
	id := scopedID(base.Scope())
	other := NewScalarField[cursorRecord, int64]("wrong", "id", codec.Signed[int64]())
	for name, q := range map[string]ProjectionQuery[cursorRecord, cursorRecord]{
		"empty keys":       base.DistinctOn(),
		"zero key":         base.DistinctOn(Group[cursorRecord]{}),
		"duplicate key":    base.DistinctOn(id.Group(), id.Group()),
		"wrong source":     base.DistinctOn(other.Group()),
		"missing metadata": For[cursorRecord]("records").Distinct(),
		"eager input":      base.With(testRelation(base, base)).Distinct(),
		"too many keys":    base.DistinctOn(make([]Group[cursorRecord], MaxExpressionNodes+1)...),
	} {
		if s, err := q.Compile(); !errors.Is(err, fault.Invalid) || s.SQL() != "" {
			t.Fatal(name, err)
		}
	}
	rank := NewNullableField[cursorRecord, int64]("records", "rank", codec.Signed[int64]())
	if _, err := base.DistinctOn(rank.Group()).OrderBy(id.Asc(), rank.Asc()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid prefix accepted", err)
	}
	if _, err := SelectValue(base, id.Value()).Distinct().OrderBy(rank.Asc()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("unselected DISTINCT ordering accepted", err)
	}
	if _, err := SelectValue(base, id.Value()).GroupBy(id.Group()).DistinctOn(rank.Group()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("ungrouped key accepted", err)
	}
	q := base.Distinct()
	q.source.node.distinct.kind = distinctKind(99)
	if _, err := q.Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid internal mode accepted", err)
	}
}

func TestDistinctCorrelatedKeysFollowOuterQualification(t *testing.T) {
	base := cursorQuery()
	a := As[firstAlias](base, "inner_record")
	link := Correlate(base, a)
	outer := scopedID(OuterScope(link, base.Scope()))
	inner := scopedID(InnerScope(link, a.Scope()))
	q := SelectCorrelatedValue(link, inner.Value()).Where(inner.EqColumn(outer)).DistinctOn(outer.Group()).OrderBy(outer.Asc(), inner.Asc())
	sub := q.correlatedValue().value.subquery
	changed := requalifyCorrelation(sub, "qualified")
	if changed.node.distinct.keys[0].(fieldRef).table != "qualified" || sub.node.distinct.keys[0].(fieldRef).table != "records" {
		t.Fatal("correlation key qualification mutated or missed outer scope")
	}
	if _, err := base.Where(q.Exists()).Compile(); err != nil {
		t.Fatal("correlated distinct did not compile", err)
	}
}

func TestDistinctOrderMatchingIsBounded(t *testing.T) {
	base := cursorQuery()
	id := scopedID(base.Scope())
	scalar := ScalarQuery(base, SelectValue(base.Where(id.Eq(1)), id.Value()))
	var c compiler
	c.sources = map[string]map[string]Column{"records": {"id": {Name: "id"}}}
	text, err := c.selectedExpression(scalar.node, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	d := distinctOrder{index: valueIndex{comparisons: MaxExpressionNodes}}
	d.add(plannedValue{sql: text, arguments: c.arguments}, 1)
	if _, err := d.compile(&c, orderNode{expression: scalar.node}, nil, false); !errors.Is(err, fault.Invalid) {
		t.Fatal("distinct match budget ignored", err)
	}
	d.index.comparisons, d.index.arguments = 0, MaxParameters
	if _, err := d.compile(&c, orderNode{expression: scalar.node}, nil, false); !errors.Is(err, fault.Invalid) {
		t.Fatal("distinct binding comparison budget ignored", err)
	}
}
