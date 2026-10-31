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

func TestNamedWindowsShareDefinitionsAndBindings(t *testing.T) {
	q := rows.From(recursionTable.Query())
	f := expr.Value(orm.KeyExpr(recursionTable.Key()))
	base := expr.WindowFor(rows.ModelScope(recursionTable.Query())).PartitionBy(expr.Group(f)).Named("partition")
	ordered := base.OrderBy(expr.Asc(f)).Named("ordered")
	framed := ordered.RowsBetween(expr.Preceding(2), expr.CurrentRow()).Named("framed")
	n := rows.SourceOf(q).Node
	n.Selections = []querycore.SelectItem{{Expression: expr.FoundrySelected(expr.RowNumber(framed), sqlowner.Expressions()).Node}, {Expression: expr.FoundrySelected(expr.Over(expr.Count[recursionRecord](), framed), sqlowner.Expressions()).Node}}
	var c querycore.Compiler
	sql, err := c.CompileSelect(n)
	if err != nil || strings.Count(sql, `"framed" AS (`) != 1 || strings.Count(sql, `OVER "framed"`) != 2 || !strings.Contains(sql, `WINDOW "partition" AS (PARTITION BY "records"."id"), "ordered" AS ("partition" ORDER BY "records"."id" ASC), "framed" AS ("ordered" ROWS BETWEEN $1 PRECEDING AND CURRENT ROW)`) || !reflect.DeepEqual(c.Arguments, []any{int64(2)}) {
		t.Fatal(sql, c.Arguments, err)
	}
	_ = ordered.RowsBetween(expr.CurrentRow(), expr.Following(9)).Named("other")
	var again querycore.Compiler
	text, err := again.CompileSelect(n)
	if err != nil || text != sql || !reflect.DeepEqual(again.Arguments, c.Arguments) {
		t.Fatal("named descriptor changed", err)
	}
	row := expr.RowNumber(framed)
	if _, err := rows.CompileRead(rows.ReadScalar(rows.OrderScalar(rows.DistinctOnScalarValues(rows.SelectScalar(q, row), expr.Key(row)), expr.AscValue(row)))); err != nil {
		t.Fatal("named DISTINCT identity", err)
	}
}
func TestNamedWindowInheritanceAndCycles(t *testing.T) {
	q := rows.From(recursionTable.Query())
	f := expr.Value(orm.KeyExpr(recursionTable.Key()))
	base := expr.WindowFor(rows.ModelScope(recursionTable.Query())).OrderBy(expr.Asc(f)).Named("ordered")
	framed := base.RowsBetween(expr.Preceding(1), expr.CurrentRow()).Named("framed")
	for name, w := range map[string]expr.Window[recursionRecord]{
		"invalid name": base.Named("bad name"), "partition override": base.PartitionBy(expr.Group(f)), "order override": base.OrderBy(expr.Desc(f)), "framed extension": framed.RowsBetween(expr.CurrentRow(), expr.UnboundedFollowing()), "framed alias": framed.Named("alias"), "nested window": expr.WindowFor(rows.ModelScope(recursionTable.Query())).OrderBy(expr.AscValue(expr.RowNumber(base))).Named("nested"),
	} {
		if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(q, expr.RowNumber(w)))); !errors.Is(err, fault.Invalid) {
			t.Fatal(name, err)
		}
	}
	other := expr.WindowFor(rows.ModelScope(recursionTable.Query())).Named("ordered")
	if _, err := rows.CompileRead(rows.ReadScalar(rows.OrderScalar(rows.SelectScalar(q, expr.RowNumber(base)), expr.AscValue(expr.RowNumber(other))))); !errors.Is(err, fault.Invalid) {
		t.Fatal("duplicate named definition", err)
	}
	cycle := &querycore.NamedWindow{Name: "cycle"}
	cycle.Definition.Reference = cycle
	n := rows.SourceOf(q).Node
	n.Selections = []querycore.SelectItem{{Expression: querycore.WindowNode{Kind: querycore.RowNumberWindow, Window: querycore.WindowSpec{Reference: cycle}}}}
	var c querycore.Compiler
	if _, err := c.CompileSelect(n); !errors.Is(err, fault.Invalid) {
		t.Fatal("cyclic window accepted", err)
	}
	grouped := expr.WindowFor(rows.ModelScope(recursionTable.Query())).PartitionByValues(expr.Key(expr.AsSelected(expr.Count[recursionRecord]()))).Named("counts")
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(q, expr.RowNumber(grouped)))); err != nil {
		t.Fatal(err)
	}
	if _, err := rows.CompileRead(rows.ReadScalar(rows.OrderScalar(rows.SelectScalar(q, expr.AsSelected(expr.Row(f))), expr.AscValue(expr.RowNumber(grouped))))); !errors.Is(err, fault.Invalid) {
		t.Fatal("named aggregate partition did not group", err)
	}
}
func TestNamedWindowQueryIsolationAndQualification(t *testing.T) {
	q := rows.From(recursionTable.Query())
	f := expr.Value(orm.KeyExpr(recursionTable.Key()))
	w := expr.WindowFor(rows.ModelScope(recursionTable.Query()))
	inner := rows.LimitScalar(rows.SelectScalar(q, expr.RowNumber(w.OrderBy(expr.Asc(f)).Named("same"))), 1)
	outer := rows.OrderScalar(rows.SelectScalar(q, expr.AsSelected(rows.ScalarRowQuery(q, inner))), expr.AscValue(expr.RowNumber(w.OrderBy(expr.Desc(f)).Named("same"))))
	s, err := rows.CompileRead(rows.ReadScalar(outer))
	if err != nil || strings.Count(s.SQL(), `WINDOW "same" AS (`) != 2 {
		t.Fatal("named definitions escaped SELECT", s.SQL(), err)
	}
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(q, expr.RowNumber(w.OrderBy(expr.AscValue(expr.AsSelected(rows.ScalarRowQuery(q, inner)))).Named("nested_scope"))))); err != nil {
		t.Fatal("independent scalar window rejected", err)
	}
}
