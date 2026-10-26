package rows

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

func TestScalarOperationsComposeAndBind(t *testing.T) {
	d := declareMembers()
	id := expr.Value(d.ID)
	text := expr.Value(d.Name)
	calculation := expr.Multiply(expr.Add(id, expr.Param(d.ID, 2)), expr.Param(d.ID, 3))
	q := expr.OrderBy(expr.Where(d.Query(), calculation.Gte(20)), calculation.Desc())
	s, err := orm.Compile(q)
	if err != nil || !strings.Contains(s.SQL(), " + ") || !strings.Contains(s.SQL(), " * ") || !reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(3), int64(20), int64(2), int64(3)}) {
		t.Fatal(s.SQL(), s.Arguments(), err)
	}
	label := expr.Lower(expr.Trim(expr.Concat(text, expr.Param(d.Name, "_%"))))
	selected := WhereScalar(SelectScalar(From(d.Query()), label.Value()), label.Contains("_%"))
	s, err = CompileRead(ReadScalar(selected))
	if err != nil || !strings.Contains(s.SQL(), "LOWER(") || !strings.Contains(s.SQL(), "BTRIM(") || !reflect.DeepEqual(s.Arguments(), []any{"_%", "_%", "%!_!%%"}) {
		t.Fatal(s.SQL(), s.Arguments(), err)
	}
	for _, read := range []Scalar[member, int64]{
		OrderScalar(GroupByScalar(SelectScalar(From(d.Query()), calculation.Value()), expr.Group(calculation)), calculation.Asc()),
		OrderScalar(DistinctOnScalarValues(SelectScalar(From(d.Query()), calculation.Value()), calculation.Value().Key()), calculation.Asc()),
	} {
		if _, err := CompileRead(ReadScalar(read)); err != nil {
			t.Fatal(err)
		}
	}
	count := expr.Count[member]()
	divided := expr.DivideValue(expr.DecimalValue(count.Value()), expr.DecimalValue(count.Param(2).Value()))
	if _, err := CompileRead(ReadScalar(HavingScalar(SelectScalar(From(d.Query()), divided), expr.OrderValue(divided).Gt(decimal.FromInt64(1))))); err != nil {
		t.Fatal("selected HAVING", err)
	}
}
func TestScalarOperationsRejectInvalidDeclarations(t *testing.T) {
	d := declareMembers()
	id := expr.Value(d.ID)
	text := expr.Value(d.Name)
	source := From(d.Query().Limit(0))
	for _, v := range []expr.Selected[member, int64]{
		expr.Add[member, int64](nil, id).Value(),
		expr.Add(id, expr.Constant[member](codec.Signed[int64]().WithParameterType(codec.TypeText), 1)).Value(),
	} {
		rejectScalar(t, source, v, codec.Signed[int64]())
	}
	for _, v := range []expr.Selected[member, string]{expr.Substring(text, 1, -1).Value(), expr.ConcatWS("\x00", text).Value()} {
		rejectScalar(t, source, v, codec.String[string]())
	}
	window := expr.AddValue(expr.RowNumber(expr.WindowFor(ModelScope(d.Query()))), expr.Param(d.ID, 1).Value())
	if _, err := CompileRead(ReadScalar(HavingScalar(SelectScalar(From(d.Query()), expr.Count[member]().Value()), expr.OrderValue(window).Gt(1)))); !errors.Is(err, fault.Invalid) {
		t.Fatal("arithmetic hid window in HAVING", err)
	}
	if _, err := CompileRead(ReadScalar(SelectScalar(From(d.Query()), expr.RowNumber(expr.WindowFor(ModelScope(d.Query())).OrderBy(window.Asc()))))); !errors.Is(err, fault.Invalid) {
		t.Fatal("arithmetic hid nested window", err)
	}
	deep := expr.Param(d.ID, 1)
	for range querycore.MaxExpressionDepth + 1 {
		deep = expr.Row(expr.Add(deep, expr.Param(d.ID, 1)))
	}
	rejectScalar(t, source, deep.Value(), codec.Signed[int64]())
	for name, node := range map[string]querycore.ValueExpression{
		"zero": querycore.OperationNode{}, "arity": querycore.OperationNode{Kind: querycore.AddOperation, Result: codec.TypeInteger},
		"result":  querycore.OperationNode{Kind: querycore.LowerOperation, Result: codec.TypeInteger, Arguments: []querycore.OperationArgument{{Value: querycore.FieldRef{Table: "members", Column: "name"}, Kind: codec.TypeText}}},
		"unknown": querycore.OperationNode{Kind: 255},
	} {
		q := SelectScalar(source, expr.Param(d.ID, 1).Value())
		q.source.Node.Selections[0].Expression = node
		if _, err := CompileRead(ReadScalar(q)); !errors.Is(err, fault.Invalid) {
			t.Fatal(name, err)
		}
	}
}
func TestScalarOrderPaginationAndAliasPreservation(t *testing.T) {
	d := declareMembers()
	id := expr.Value(d.ID)
	computed := expr.Add(id, expr.Param(d.ID, 1))
	order := computed.Asc()
	copy := order
	if !reflect.DeepEqual(order, copy) {
		t.Fatal("copy changed order")
	}
	q := expr.OrderBy(d.Query(), computed.Desc(), expr.Subtract(id, expr.Param(d.ID, 2)).Asc())
	state := &driverState{columns: []string{"id", "name", "nickname"}}
	if _, err := q.SimplePaginate(t.Context(), openTest(t, state), orm.Page(1, 2)); err != nil || !strings.Contains(state.sql, `"members"."id" ASC LIMIT`) {
		t.Fatal("stable order lost primary", state.sql, err)
	}
	state = &driverState{}
	if _, err := orm.CursorPaginate(t.Context(), openTest(t, state), q, orm.CursorRequest[member]{Size: 2}); !errors.Is(err, fault.Invalid) || state.sql != "" {
		t.Fatal("computed model cursor accepted", err)
	}
	state = &driverState{columns: []string{"id", "name", "nickname"}}
	if _, err := orm.CursorPaginate(t.Context(), openTest(t, state), expr.OrderBy(d.Query(), expr.OrderRow(id).Asc()), orm.CursorRequest[member]{Size: 2}); err != nil {
		t.Fatal("plain row lost cursor", err)
	}
	original := expr.FoundrySelected(computed.Value(), sqlowner.Expressions()).Node
	changed := querycore.RequalifyValue(original, "other").(querycore.OperationNode)
	if changed.Arguments[0].Value.(querycore.FieldRef).Table != "other" || original.(querycore.OperationNode).Arguments[0].Value.(querycore.FieldRef).Table != "members" {
		t.Fatal("operation requalification mutated source")
	}
}
