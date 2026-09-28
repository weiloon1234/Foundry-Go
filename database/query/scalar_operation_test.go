package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestScalarOperationsComposeAndBind(t *testing.T) {
	q := cursorQuery()
	id := NewExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	text := NewTextField[cursorRecord, string]("records", "rank", codec.String[string]())
	calculation := Multiply(Add(id, id.Param(2)), id.Param(3))
	statement, err := q.Where(calculation.Gte(20)).OrderBy(calculation.Desc()).Compile()
	if err != nil || !strings.Contains(statement.SQL(), " + ") || !strings.Contains(statement.SQL(), " * ") || !reflect.DeepEqual(statement.Arguments(), []any{int64(2), int64(3), int64(20), int64(2), int64(3)}) {
		t.Fatal(statement.SQL(), statement.Arguments(), err)
	}
	label := Lower(Trim(Concat(text, text.Param("_%"))))
	s, err := SelectValue(q.Where(label.Contains("_%")), label.Value()).Compile()
	if err != nil || !strings.Contains(s.SQL(), "LOWER(") || !strings.Contains(s.SQL(), "BTRIM(") || !reflect.DeepEqual(s.Arguments(), []any{"_%", "_%", "%!_!%%"}) {
		t.Fatal(s.SQL(), s.Arguments(), err)
	}
	if _, err := SelectValue(q, calculation.Value()).GroupBy(calculation.Group()).OrderBy(calculation.Asc()).Compile(); err != nil {
		t.Fatal("computed grouping", err)
	}
	if _, err := SelectValue(q, calculation.Value()).DistinctOnValues(calculation.Value().Key()).OrderBy(calculation.Asc()).Compile(); err != nil {
		t.Fatal("computed distinct identity", err)
	}
	count := Count[cursorRecord]()
	selected := DivideValue(DecimalValue(count.Value()), DecimalValue(count.Param(2).Value()))
	if _, err := SelectValue(q, selected).Having(OrderValue(selected).Gt(decimal.FromInt64(1))).Compile(); err != nil {
		t.Fatal("selected arithmetic HAVING", err)
	}
}

func TestScalarOperationsRejectInvalidDeclarations(t *testing.T) {
	id := NewExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	text := NewTextField[cursorRecord, string]("records", "rank", codec.String[string]())
	badRepresentation := NewExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]().WithParameterType(codec.TypeText))
	for name, node := range map[string]valueExpression{
		"zero":              operationNode{},
		"arity":             operationNode{kind: addOperation, result: codec.TypeInteger},
		"result":            operationNode{kind: lowerOperation, result: codec.TypeInteger, arguments: []operationArgument{operationArg(text.Value())}},
		"type":              Add(id, badRepresentation).Value().node,
		"null descriptor":   Add[cursorRecord, int64](nil, id).Value().node,
		"negative count":    Substring(text, 1, -1).Value().node,
		"invalid separator": ConcatWS("\x00", text).Value().node,
		"unknown operation": operationNode{kind: 255},
	} {
		if _, err := SelectValue(cursorQuery().Limit(0), Expression[cursorRecord, int64]{node: node, codec: codec.Signed[int64]()}).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal(name, err)
		}
	}
	window := AddValue(RowNumber(WindowFor(cursorQuery())), id.Param(1).Value())
	if _, err := SelectValue(cursorQuery(), Count[cursorRecord]().Value()).Having(OrderValue(window).Gt(1)).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("arithmetic hid a window in HAVING", err)
	}
	if _, err := SelectValue(cursorQuery(), RowNumber(WindowFor(cursorQuery()).OrderBy(window.Asc()))).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("arithmetic hid a nested window", err)
	}
	deep := id.Param(1)
	for range MaxExpressionDepth + 1 {
		deep = Add(deep, id.Param(1)).RowExpression
	}
	if _, err := SelectValue(cursorQuery(), deep.Value()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbounded arithmetic accepted", err)
	}
}

func TestScalarOrderPaginationAndAliasPreservation(t *testing.T) {
	id := NewExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	computed := Add(id, id.Param(1))
	order := computed.Asc()
	copy := order
	if order != copy {
		t.Fatal("copied order lost identity")
	}
	q := cursorQuery().OrderBy(computed.Desc(), Subtract(id, id.Param(2)).Asc())
	ordered, err := q.stableModelOrder()
	if err != nil || len(ordered.orders) != 3 || ordered.orders[2].field.column != "id" {
		t.Fatal("computed ordering lost primary tie-breaker", err)
	}
	if _, err := q.cursorPlan(CursorRequest[cursorRecord]{Size: 2}); !errors.Is(err, fault.Invalid) {
		t.Fatal("computed key silently entered model cursor", err)
	}
	if _, err := cursorQuery().OrderBy(OrderRow(id).Asc()).cursorPlan(CursorRequest[cursorRecord]{Size: 2}); err != nil {
		t.Fatal("plain row field lost cursor support", err)
	}
	node := requalifyValue(computed.Value().node, "other")
	op := node.(operationNode)
	if op.arguments[0].value.(fieldRef).table != "other" || computed.Value().node.(operationNode).arguments[0].value.(fieldRef).table != "records" {
		t.Fatal("operation qualification changed its source")
	}
}
