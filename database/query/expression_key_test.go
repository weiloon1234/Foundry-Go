package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestComputedGroupKeyBindingsAndBoundaries(t *testing.T) {
	id := NewExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	key := When(id.Gt(1), id.Param(2)).Else(id.Param(3))
	q := SelectValue(cursorQuery(), key.Value()).GroupBy(key.Group()).Having(Grouped(key.Ne(0))).OrderBy(key.Desc())
	s, err := q.Compile()
	if err != nil {
		t.Fatal(err)
	}
	fragment := `CASE WHEN ("records"."id" > $1) THEN CAST($2 AS bigint) ELSE CAST($3 AS bigint) END`
	if strings.Count(s.SQL(), fragment) != 4 || !reflect.DeepEqual(s.Arguments(), []any{int64(1), int64(2), int64(3), int64(0)}) {
		t.Fatal("group key lost parameter identity", s.SQL(), s.Arguments())
	}
	if _, err := SelectValue(cursorQuery(), id.Value()).GroupBy(key.Group()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("computed grouping made input columns selectable", err)
	}
	equal := When(id.Gt(1), id.Param(2)).Else(id.Param(3))
	if _, err := SelectValue(cursorQuery(), equal.Value()).GroupBy(key.Group()).Compile(); err != nil {
		t.Fatal("equivalent declaration did not match", err)
	}
	different := When(id.Gt(1), id.Param(2)).Else(id.Param(4))
	if _, err := SelectValue(cursorQuery(), different.Value()).GroupBy(key.Group()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("group identity ignored parameters", err)
	}
	if _, err := q.GroupBy(equal.Group()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("duplicate computed group accepted", err)
	}
	outer := NullIf(key, id.Param(2))
	if _, err := SelectValue(cursorQuery(), outer.Value()).GroupBy(key.Group()).Compile(); err != nil {
		t.Fatal("grouped subexpression was not recognized", err)
	}
}

func TestComputedDistinctAndPartitionKeys(t *testing.T) {
	id := NewExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	key := When(id.Gt(1), id.Param(2)).Else(id.Param(3))
	q := cursorQuery().DistinctOn(key.Group()).OrderBy(key.Asc(), id.Desc())
	s, err := q.Compile()
	if err != nil || len(s.Arguments()) != 3 || !strings.Contains(s.SQL(), "DISTINCT ON (CASE") {
		t.Fatal("computed DISTINCT ON lost bindings", s.SQL(), s.Arguments(), err)
	}
	other := When(id.Gt(1), id.Param(2)).Else(id.Param(4))
	if _, err := cursorQuery().DistinctOn(key.Group()).OrderBy(other.Asc()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("DISTINCT ordering ignored key parameters", err)
	}
	window := WindowFor(cursorQuery()).PartitionBy(key.Group()).OrderBy(id.Asc())
	if _, err := SelectValue(cursorQuery(), RowNumber(window)).Compile(); err != nil {
		t.Fatal(err)
	}
	if _, err := SelectValue(cursorQuery(), RowNumber(window.PartitionBy(key.Group()))).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("duplicate computed partition accepted", err)
	}
	count := Count[cursorRecord]()
	if _, err := SelectValue(cursorQuery(), RowNumber(WindowFor(cursorQuery()).PartitionByValues(count.Value().Key()))).Compile(); err != nil {
		t.Fatal("aggregate partition did not establish grouping", err)
	}
	if _, err := SelectValue(cursorQuery(), id.Value()).DistinctOnValues(count.Value().Key()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("aggregate DISTINCT key failed to establish grouping", err)
	}
	if _, err := SelectValue(cursorQuery(), count.Value()).DistinctOnValues(count.Value().Key()).Compile(); err != nil {
		t.Fatal("selected aggregate key failed", err)
	}
	if _, err := SelectValue(cursorQuery(), RowNumber(window.PartitionByValues(RowNumber(window).Key()))).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("partition concealed nested window", err)
	}
}

func TestGroupedKeyReuseInsideDistinctAndEmptyMembership(t *testing.T) {
	id := NewExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	key := When(id.Gt(1), id.Param(2)).Else(id.Param(3))
	outer := NullIf(key, id.Param(4)).Value()
	q := SelectValue(cursorQuery(), outer).GroupBy(key.Group()).DistinctOnValues(outer.Key()).OrderBy(outer.Asc())
	s, err := q.Compile()
	if err != nil || len(s.Arguments()) != 4 {
		t.Fatal("distinct expression rebound its grouped child", s.SQL(), s.Arguments(), err)
	}
	constant := id.Param(1)
	choice := When(constant.In(), id.Param(9)).Else(id.Param(7))
	s, err = SelectValue(cursorQuery(), choice.Value()).GroupBy(constant.Group()).Compile()
	if err != nil || !reflect.DeepEqual(s.Arguments(), []any{int64(9), int64(7), int64(1)}) || !strings.Contains(s.SQL(), "GROUP BY CAST($3 AS bigint)") {
		t.Fatal("discarded operand left a cached parameter reference", s.SQL(), s.Arguments(), err)
	}
	if _, err := SelectValue(cursorQuery(), outer).GroupBy(key.Group()).Distinct().OrderBy(outer.Asc()).Compile(); err != nil {
		t.Fatal("ordinary DISTINCT failed on reused grouped parameters", err)
	}
}

func TestCanonicalValuePlanningIsIsolated(t *testing.T) {
	input := `"$10", 'it''s $11', $123`
	p := NewScalarField[cursorRecord, string]("records", "unused", codec.String[string]()).Param(input)
	c := compiler{arguments: []any{int64(8)}}
	plan, err := c.planValue(p.Value().node)
	if err != nil || plan.sql != "CAST($1 AS text)" || !reflect.DeepEqual(plan.arguments, []any{input}) || !reflect.DeepEqual(c.arguments, []any{int64(8)}) {
		t.Fatal("value planning changed statement bindings or text", plan, c.arguments, err)
	}
}
