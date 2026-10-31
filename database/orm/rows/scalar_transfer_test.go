package rows_test

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

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
	id := expr.Value(orm.KeyExpr(d.ID))
	text := expr.Value(orm.TextExpr(d.Name))
	calculation := expr.Multiply(expr.Add(id, expr.Param(orm.KeyExpr(d.ID), 2)), expr.Param(orm.KeyExpr(d.ID), 3))
	q := expr.OrderBy(expr.Where(d.Query(), expr.Gte(calculation, 20)), expr.Desc(calculation))
	s, err := orm.Compile(q)
	if err != nil || !strings.Contains(s.SQL(), " + ") || !strings.Contains(s.SQL(), " * ") || !reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(3), int64(20), int64(2), int64(3)}) {
		t.Fatal(s.SQL(), s.Arguments(), err)
	}
	label := expr.Lower(expr.Trim(expr.Concat(text, expr.Param(orm.TextExpr(d.Name), "_%"))))
	selected := rows.WhereScalar(rows.SelectScalar(rows.From(d.Query()), expr.AsSelected(label)), expr.TextContains(label, "_%"))
	s, err = rows.CompileRead(rows.ReadScalar(selected))
	if err != nil || !strings.Contains(s.SQL(), "LOWER(") || !strings.Contains(s.SQL(), "BTRIM(") || !reflect.DeepEqual(s.Arguments(), []any{"_%", "_%", "%!_!%%"}) {
		t.Fatal(s.SQL(), s.Arguments(), err)
	}
	for _, read := range []rows.Scalar[member, int64]{
		rows.OrderScalar(rows.GroupByScalar(rows.SelectScalar(rows.From(d.Query()), expr.AsSelected(calculation)), expr.Group(calculation)), expr.Asc(calculation)),
		rows.OrderScalar(rows.DistinctOnScalarValues(rows.SelectScalar(rows.From(d.Query()), expr.AsSelected(calculation)), expr.Key(expr.AsSelected(calculation))), expr.Asc(calculation)),
	} {
		if _, err := rows.CompileRead(rows.ReadScalar(read)); err != nil {
			t.Fatal(err)
		}
	}
	count := expr.Count[member]()
	divided := expr.DivideValue(expr.DecimalValue(expr.AsSelected(count)), expr.DecimalValue(expr.AsSelected(expr.ParamOf(count, 2))))
	if _, err := rows.CompileRead(rows.ReadScalar(rows.HavingScalar(rows.SelectScalar(rows.From(d.Query()), divided), expr.GtValue(expr.OrderValue(divided), decimal.FromInt64(1))))); err != nil {
		t.Fatal("selected HAVING", err)
	}
}
func TestScalarOperationsRejectInvalidDeclarations(t *testing.T) {
	d := declareMembers()
	id := expr.Value(orm.KeyExpr(d.ID))
	text := expr.Value(orm.TextExpr(d.Name))
	source := rows.From(d.Query().Limit(0))
	for _, v := range []expr.Selected[member, int64]{
		expr.AsSelected(expr.Add[member, int64](nil, id)),
		expr.AsSelected(expr.Add(id, expr.Constant[member](codec.Signed[int64]().WithParameterType(codec.TypeText), 1))),
	} {
		rejectScalar(t, source, v, codec.Signed[int64]())
	}
	for _, v := range []expr.Selected[member, string]{expr.AsSelected(expr.Substring(text, 1, -1)), expr.AsSelected(expr.ConcatWS("\x00", text))} {
		rejectScalar(t, source, v, codec.String[string]())
	}
	window := expr.AddValue(expr.RowNumber(expr.WindowFor(rows.ModelScope(d.Query()))), expr.AsSelected(expr.Param(orm.KeyExpr(d.ID), 1)))
	if _, err := rows.CompileRead(rows.ReadScalar(rows.HavingScalar(rows.SelectScalar(rows.From(d.Query()), expr.AsSelected(expr.Count[member]())), expr.GtValue(expr.OrderValue(window), 1)))); !errors.Is(err, fault.Invalid) {
		t.Fatal("arithmetic hid window in HAVING", err)
	}
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(d.Query()), expr.RowNumber(expr.WindowFor(rows.ModelScope(d.Query())).OrderBy(expr.AscValue(window)))))); !errors.Is(err, fault.Invalid) {
		t.Fatal("arithmetic hid nested window", err)
	}
	deep := expr.Param(orm.KeyExpr(d.ID), 1)
	for range querycore.MaxExpressionDepth + 1 {
		deep = expr.Row(expr.Add(deep, expr.Param(orm.KeyExpr(d.ID), 1)))
	}
	rejectScalar(t, source, expr.AsSelected(deep), codec.Signed[int64]())
	for name, node := range map[string]querycore.ValueExpression{
		"zero": querycore.OperationNode{}, "arity": querycore.OperationNode{Kind: querycore.AddOperation, Result: codec.TypeInteger},
		"result":  querycore.OperationNode{Kind: querycore.LowerOperation, Result: codec.TypeInteger, Arguments: []querycore.OperationArgument{{Value: querycore.FieldRef{Table: "members", Column: "name"}, Kind: codec.TypeText}}},
		"unknown": querycore.OperationNode{Kind: 255},
	} {
		q := rows.SelectScalar(source, expr.AsSelected(expr.Param(orm.KeyExpr(d.ID), 1)))
		rows.ScalarSource(&q).Node.Selections[0].Expression = node
		if _, err := rows.CompileRead(rows.ReadScalar(q)); !errors.Is(err, fault.Invalid) {
			t.Fatal(name, err)
		}
	}
}
func TestScalarOrderPaginationAndAliasPreservation(t *testing.T) {
	d := declareMembers()
	id := expr.Value(orm.KeyExpr(d.ID))
	computed := expr.Add(id, expr.Param(orm.KeyExpr(d.ID), 1))
	order := expr.Asc(computed)
	copy := order
	if !reflect.DeepEqual(order, copy) {
		t.Fatal("copy changed order")
	}
	q := expr.OrderBy(d.Query(), expr.Desc(computed), expr.Asc(expr.Subtract(id, expr.Param(orm.KeyExpr(d.ID), 2))))
	state := &driverState{columns: []string{"id", "name", "nickname"}}
	if _, err := orm.SimplePaginate(t.Context(), openTest(t, state), q, orm.Page(1, 2)); err != nil || !strings.Contains(state.sql, `"members"."id" ASC LIMIT`) {
		t.Fatal("stable order lost primary", state.sql, err)
	}
	state = &driverState{}
	if _, err := orm.CursorPaginate(t.Context(), openTest(t, state), q, orm.CursorRequest[member]{Size: 2}); !errors.Is(err, fault.Invalid) || state.sql != "" {
		t.Fatal("computed model cursor accepted", err)
	}
	state = &driverState{columns: []string{"id", "name", "nickname"}}
	if _, err := orm.CursorPaginate(t.Context(), openTest(t, state), expr.OrderBy(d.Query(), expr.Asc(expr.OrderRow(id))), orm.CursorRequest[member]{Size: 2}); err != nil {
		t.Fatal("plain row lost cursor", err)
	}
	original := expr.FoundrySelected(expr.AsSelected(computed), sqlowner.Expressions()).Node
	changed := querycore.RequalifyValue(original, "other").(querycore.OperationNode)
	if changed.Arguments[0].Value.(querycore.FieldRef).Table != "other" || original.(querycore.OperationNode).Arguments[0].Value.(querycore.FieldRef).Table != "members" {
		t.Fatal("operation requalification mutated source")
	}
}
