package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestValueComparisonsShareSQLAndBindings(t *testing.T) {
	q := cursorQuery()
	id := NewExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	for _, test := range []struct {
		predicate Predicate[cursorRecord]
		sql       string
	}{
		{Equal(id, id.Param(2)), "="}, {NotEqual(id, id.Param(2)), "<>"},
		{Less(id, id.Param(2)), "<"}, {LessOrEqual(id, id.Param(2)), "<="},
		{Greater(id, id.Param(2)), ">"}, {GreaterOrEqual(id, id.Param(2)), ">="},
		{DistinctFrom(id, id.Param(2)), "IS DISTINCT FROM"}, {NotDistinctFrom(id, id.Param(2)), "IS NOT DISTINCT FROM"},
	} {
		s, err := q.Where(test.predicate).Compile()
		if err != nil || !strings.Contains(s.SQL(), test.sql+" CAST($1 AS bigint)") || !reflect.DeepEqual(s.Arguments(), []any{int64(2)}) {
			t.Fatal(s.SQL(), s.Arguments(), err)
		}
	}
	s, err := q.Where(Equal(Add(id, id.Param(2)), Multiply(id, id.Param(3)))).Compile()
	if err != nil || !reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(3)}) || !strings.Contains(s.SQL(), " + ") || !strings.Contains(s.SQL(), " * ") {
		t.Fatal(s.SQL(), s.Arguments(), err)
	}
	name := NewTextField[cursorRecord, string]("records", "rank", codec.String[string]())
	s, err = q.Where(Like(Lower(name), name.Param("%_!"))).Compile()
	if err != nil || !strings.Contains(s.SQL(), " LIKE CAST($1 AS text)") || !reflect.DeepEqual(s.Arguments(), []any{"%_!"}) {
		t.Fatal(s.SQL(), s.Arguments(), err)
	}
}

func TestValueComparisonPhasesAndInvalidOperands(t *testing.T) {
	q := cursorQuery()
	id := NewExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	rank := NewNullableExactField[cursorRecord, int64]("records", "rank", codec.Signed[int64]())
	count := Count[cursorRecord]()
	for _, p := range []HavingPredicate[cursorRecord]{
		GreaterValue(count.Value(), count.Param(1).Value()),
		EqualValue(count.Value(), count.Value()),
		LessNullableValue(rank.Sum().Value(), rank.Sum().Value()),
	} {
		if _, err := SelectValue(q, count.Value()).Having(p).Compile(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := SelectValue(q, id.Value()).Having(GreaterValue(count.Value(), count.Param(1).Value())).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("aggregate in binary HAVING failed to establish grouping", err)
	}
	window := RowNumber(WindowFor(q))
	for _, p := range []HavingPredicate[cursorRecord]{EqualValue(window, count.Value()), EqualValue(count.Value(), window)} {
		if _, err := SelectValue(q, count.Value()).Having(p).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal("window in HAVING", err)
		}
	}
	for _, p := range []Predicate[cursorRecord]{
		Equal[cursorRecord, int64](nil, id),
		Equal[cursorRecord, int64](id, nil),
		{expression: binaryComparison{id.ref, id.ref, contains}},
		{expression: binaryComparison{id.ref, id.ref, insensitiveContains}},
		{expression: binaryComparison{id.ref, id.ref, 255}},
		{expression: binaryComparison{id.ref, count.Value().node, equal}},
		{expression: binaryComparison{count.Value().node, id.ref, equal}},
		Equal(id, NewExactField[cursorRecord, int64]("wrong", "id", codec.Signed[int64]())),
		Equal(ScalarRowQuery[cursorRecord, int64](q, nil), NullableRow(id)),
	} {
		if _, err := q.Limit(0).Where(p).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid operand accepted", err)
		}
	}
}

func TestComputedJoinAndCrossBoundaries(t *testing.T) {
	a, b := As[firstAlias](cursorQuery(), "a"), As[secondAlias](cursorQuery(), "b")
	x, y := scopedID(a.Scope()), scopedID(b.Scope())
	j := LeftJoin(a, b, OnAnd(OnLess(Add(x, x.Param(1)), y), OnNotEqual(x, y)))
	l, r := scopedID(LeftScope(j, a.Scope())), nullableScopedID(NullableRightScope(j, b.Scope()))
	s, err := pairProjection(j, Nullable(l.Value()), r.Value()).Compile()
	if err != nil || !strings.Contains(s.SQL(), " < \"b\".\"id\"") || !reflect.DeepEqual(s.Arguments(), []any{int64(1)}) {
		t.Fatal(s.SQL(), s.Arguments(), err)
	}
	cross := CrossJoin(a, b)
	cx, cy := scopedID(LeftScope(cross, a.Scope())), scopedID(RightScope(cross, b.Scope()))
	selected := pairProjection(cross, Nullable(cx.Value()), Nullable(cy.Value()))
	s, err = selected.Compile()
	if err != nil || !strings.Contains(s.SQL(), " CROSS JOIN \"records\" AS \"b\"") || strings.Contains(s.SQL(), " ON ") {
		t.Fatal(s.SQL(), err)
	}
	for _, bad := range []joinNode{
		{source: b.input.node.source, kind: crossJoin, on: On(x, y).expression},
		{source: b.input.node.source, kind: innerJoin},
		{source: b.input.node.source, kind: 255},
	} {
		invalid := selected
		invalid.source.node.joins = []joinNode{bad}
		if _, err := invalid.Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid join accepted", err)
		}
	}
	chain := LeftJoin(cross, As[thirdAlias](cursorQuery(), "c"), On(cx, scopedID(As[thirdAlias](cursorQuery(), "c").Scope())))
	if _, err := pairProjection(chain, Nullable(scopedID(LeftScope(chain, LeftScope(cross, a.Scope()))).Value()), Nullable(scopedID(LeftScope(chain, RightScope(cross, b.Scope()))).Value())).Compile(); err != nil {
		t.Fatal(err)
	}
}

func TestBinaryComparisonRequalificationOwnsStructure(t *testing.T) {
	id := NewExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	original := Equal(Add(id, id.Param(1)), Subtract(id, id.Param(2))).expression
	changed := requalify(original, "other")
	collect := func(e expression) []string {
		var tables []string
		w := selectWalk{field: func(f fieldRef) { tables = append(tables, f.table) }}
		w.expression(e, 0)
		if w.err != nil {
			t.Fatal(w.err)
		}
		return tables
	}
	if !reflect.DeepEqual(collect(original), []string{"records", "records"}) || !reflect.DeepEqual(collect(changed), []string{"other", "other"}) {
		t.Fatal("comparison rewrite mutated source or skipped operand")
	}
}
