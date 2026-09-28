package query

import (
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestConditionalValuesBindingsAndImmutability(t *testing.T) {
	id := NewExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	rank := NewNullableOrderedField[cursorRecord, int64]("records", "rank", codec.Signed[int64]())
	base := When(id.Eq(1), id.Param(10))
	first := base.Else(id.Param(20))
	second := base.When(id.Eq(2), id.Param(30)).Else(id.Param(40))
	if len(base.builder.branches) != 1 || len(first.Value().node.(caseNode).branches) != 1 || len(second.Value().node.(caseNode).branches) != 2 {
		t.Fatal("CASE derivation mutated branches")
	}
	s, err := SelectValue(cursorQuery().Where(Coalesce(rank, id).Eq(5)), first.Value()).OrderBy(first.Asc()).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.SQL(), `CASE WHEN ("records"."id" = $1) THEN CAST($2 AS bigint) ELSE CAST($3 AS bigint) END`) || !reflect.DeepEqual(s.Arguments(), []any{int64(1), int64(10), int64(20), int64(5), int64(1), int64(10), int64(20)}) {
		t.Fatal("conditional bindings", s.SQL(), s.Arguments())
	}
	if _, err := SelectValue(cursorQuery(), NullIf(Coalesce(rank, id), id).Value()).Compile(); err != nil {
		t.Fatal(err)
	}
	if _, err := SelectValue(cursorQuery(), (Case[cursorRecord, int64]{}).When(id.Eq(1), id).ElseNull().Value()).Compile(); err != nil {
		t.Fatal("zero CASE could not acquire its first branch", err)
	}
	if _, err := SelectValue(cursorQuery(), base.ElseNull().Value()).Compile(); err != nil {
		t.Fatal(err)
	}
	// DISTINCT ordering reuses the selected expression and its parameter identity.
	s, err = SelectValue(cursorQuery(), first.Value()).Distinct().OrderBy(first.Asc()).Compile()
	if err != nil || !strings.Contains(s.SQL(), "ORDER BY 1 ASC") || len(s.Arguments()) != 3 {
		t.Fatal("distinct computed order rebound parameters", s.SQL(), err)
	}
}

func TestConditionalValuePhaseAndBounds(t *testing.T) {
	id := NewExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	q := cursorQuery()
	count := Count[cursorRecord]()
	selected := WhenValue(count.Gt(0), count.Value()).Else(count.Param(0).Value())
	if _, err := SelectValue(q, selected).Compile(); err != nil {
		t.Fatal(err)
	}
	grouped := WhenValue(Grouped(id.Eq(1)), count.Value()).Else(count.Param(0).Value())
	if _, err := SelectValue(q, grouped).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("ungrouped CASE condition accepted", err)
	}
	if _, err := SelectValue(q, grouped).GroupBy(id.Group()).Compile(); err != nil {
		t.Fatal(err)
	}
	window := RowNumber(WindowFor(q).OrderBy(id.Asc()))
	wrapped := WhenValue(Grouped(id.Gt(0)), window).Else(window.Param(0).Value())
	if _, err := SelectValue(q, wrapped).Compile(); err != nil {
		t.Fatal(err)
	}
	if _, err := SelectValue(q, Lag(wrapped, 1, WindowFor(q))).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("CASE concealed nested window", err)
	}
	having := HavingPredicate[cursorRecord]{expression: typedComparison(wrapped.node, equal, codec.Signed[int64](), []int64{1})}
	if _, err := SelectValue(q, count.Value()).Having(having).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("CASE concealed window in HAVING", err)
	}
	for name, node := range map[string]valueExpression{
		"empty CASE":       caseNode{},
		"empty coalesce":   conditionalNode{kind: coalesceValues},
		"invalid NULLIF":   conditionalNode{kind: nullIfValues, arguments: []valueExpression{id.ref}},
		"nested aggregate": conditionalNode{kind: coalesceValues, arguments: []valueExpression{count.node}},
		"nested window":    conditionalNode{kind: coalesceValues, arguments: []valueExpression{window.node}},
	} {
		predicate := Predicate[cursorRecord]{expression: typedComparison(node, equal, codec.Signed[int64](), []int64{1})}
		if _, err := q.Where(predicate).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal(name, err)
		}
	}
	cyclic := conditionalNode{kind: coalesceValues, arguments: make([]valueExpression, 1)}
	cyclic.arguments[0] = cyclic
	if _, err := SelectValue(q, Expression[cursorRecord, int64]{node: cyclic, codec: codec.Signed[int64]()}).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("conditional cycle accepted", err)
	}
	if _, err := q.Where(Predicate[cursorRecord]{expression: typedComparison(cyclic, equal, codec.Signed[int64](), []int64{1})}).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("row-value cycle accepted", err)
	}
	items := make([]valueExpression, MaxExpressionNodes+1)
	if _, err := SelectValue(q, Expression[cursorRecord, int64]{node: conditionalNode{kind: coalesceValues, arguments: items}, codec: codec.Signed[int64]()}).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbounded arguments accepted", err)
	}
	if _, err := SelectValue(q, When(id.Eq(1), NullableRow(id)).ElseNull().Value()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("nested NULL wrappers accepted", err)
	}
	if _, err := SelectValue(q, NullIf(NullableRow(id), NullableRow(id)).Value()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("nullable NULLIF omitted its explicit variant", err)
	}
}

func TestParameterCaptureAndCodecTypes(t *testing.T) {
	encodings := 0
	c := codec.New(func(v *[2]byte) (driver.Value, error) { encodings++; return v[:], nil }, func(any) (*[2]byte, error) { return nil, nil }).WithParameterType(codec.TypeBytes)
	original := &[2]byte{1, 2}
	p := parameterExpression[cursorRecord](original, c)
	original[0] = 9
	first, err := SelectValue(cursorQuery(), p.Value()).Compile()
	if err != nil {
		t.Fatal(err)
	}
	first.Arguments()[0].([]byte)[0] = 8
	first.arguments[0].([]byte)[0] = 7 // even an internal executor's buffer must not alias the declaration
	second, err := SelectValue(cursorQuery(), p.Value()).Compile()
	if err != nil || encodings != 1 || !reflect.DeepEqual(second.Arguments(), []any{[]byte{1, 2}}) {
		t.Fatal("parameter capture/ownership", second.Arguments(), encodings, err)
	}
	for _, c := range []codec.Codec[string]{codec.New(func(v string) (driver.Value, error) { return v, nil }, func(any) (string, error) { return "", nil }), codec.String[string]().WithParameterType(255), (codec.Codec[string]{}).WithParameterType(codec.TypeText)} {
		if _, err := SelectValue(cursorQuery(), parameterExpression[cursorRecord]("x", c).Value()).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid parameter metadata accepted", err)
		}
	}
	invalid := NewScalarField[cursorRecord, string]("records", "unused", codec.String[string]()).Param("\x00")
	if _, err := SelectValue(cursorQuery(), invalid.Value()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid parameter value accepted", err)
	}
	if got := codec.Nullable(codec.Signed[int64]().Validated(func(int64) error { return nil })).ParameterType(); got != codec.TypeInteger {
		t.Fatal("codec adapter discarded parameter type", got)
	}
	id := NewScalarField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	if _, err := cursorQuery().Where(NullFor(id).Eq(value.Null[int64]())).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("literal NULL equality was not rejected", err)
	}
}

func TestEmptyMembershipDiscardsComputedBindings(t *testing.T) {
	id := NewExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	choice := When(id.Gt(1), id.Param(2)).Else(id.Param(3))
	q := SelectValue(cursorQuery().Where(id.Eq(4), choice.In().Not(), id.Eq(5)), id.Param(6).Value())
	s, err := q.Compile()
	if err != nil || !reflect.DeepEqual(s.Arguments(), []any{int64(6), int64(4), int64(5)}) || !strings.Contains(s.SQL(), `"records"."id" = $3`) || strings.Contains(s.SQL(), "CASE") {
		t.Fatal("empty IN retained discarded operand bindings", s.SQL(), s.Arguments(), err)
	}
	invalid := NewScalarField[cursorRecord, string]("records", "unused", codec.String[string]()).Param("\x00")
	if _, err := cursorQuery().Where(invalid.In()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("empty IN concealed invalid operand", err)
	}
}
