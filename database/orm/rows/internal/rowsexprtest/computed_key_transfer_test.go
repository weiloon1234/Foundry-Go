package rowsexprtest

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"reflect"
	"strings"
	"testing"
)

func TestComputedGroupKeyBindingsAndBoundaries(t *testing.T) {
	d := declareMembers()
	id := expr.Value(orm.KeyExpr(d.ID))
	key := expr.When(expr.Gt(expr.OrderRow(id), 1), expr.Param(orm.KeyExpr(d.ID), 2)).Else(expr.Param(orm.KeyExpr(d.ID), 3))
	q := rows.OrderScalar(rows.HavingScalar(rows.GroupByScalar(rows.SelectScalar(rows.From(d.Query()), expr.AsSelected(key)), expr.Group(key)), expr.Grouped(expr.Ne(key, 0))), expr.Desc(key))
	s, err := rows.CompileRead(rows.ReadScalar(q))
	fragment := `CASE WHEN ("members"."id" > $1) THEN CAST($2 AS bigint) ELSE CAST($3 AS bigint) END`
	if err != nil || strings.Count(s.SQL(), fragment) != 4 || !reflect.DeepEqual(s.Arguments(), []any{int64(1), int64(2), int64(3), int64(0)}) {
		t.Fatal("group lost parameter identity", s.SQL(), s.Arguments(), err)
	}
	if _, err := rows.CompileRead(rows.ReadScalar(rows.GroupByScalar(rows.SelectScalar(rows.From(d.Query()), expr.AsSelected(expr.Row(id))), expr.Group(key)))); !errors.Is(err, fault.Invalid) {
		t.Fatal("computed group selected ungrouped input", err)
	}
	equal := expr.When(expr.Gt(expr.OrderRow(id), 1), expr.Param(orm.KeyExpr(d.ID), 2)).Else(expr.Param(orm.KeyExpr(d.ID), 3))
	different := expr.When(expr.Gt(expr.OrderRow(id), 1), expr.Param(orm.KeyExpr(d.ID), 2)).Else(expr.Param(orm.KeyExpr(d.ID), 4))
	if _, err := rows.CompileRead(rows.ReadScalar(rows.GroupByScalar(rows.SelectScalar(rows.From(d.Query()), expr.AsSelected(equal)), expr.Group(key)))); err != nil {
		t.Fatal("equivalent declarations differ", err)
	}
	if _, err := rows.CompileRead(rows.ReadScalar(rows.GroupByScalar(rows.SelectScalar(rows.From(d.Query()), expr.AsSelected(different)), expr.Group(key)))); !errors.Is(err, fault.Invalid) {
		t.Fatal("group ignored parameters", err)
	}
	if _, err := rows.CompileRead(rows.ReadScalar(rows.GroupByScalar(q, expr.Group(equal)))); !errors.Is(err, fault.Invalid) {
		t.Fatal("duplicate group", err)
	}
	outer := expr.NullIf(key, expr.Param(orm.KeyExpr(d.ID), 2))
	if _, err := rows.CompileRead(rows.ReadScalar(rows.GroupByScalar(rows.SelectScalar(rows.From(d.Query()), expr.AsSelected(outer)), expr.Group(key)))); err != nil {
		t.Fatal("grouped subexpression missed", err)
	}
}
func TestComputedDistinctAndPartitionKeys(t *testing.T) {
	d := declareMembers()
	id := expr.Value(orm.KeyExpr(d.ID))
	base := rows.From(d.Query())
	key := expr.When(expr.Gt(expr.OrderRow(id), 1), expr.Param(orm.KeyExpr(d.ID), 2)).Else(expr.Param(orm.KeyExpr(d.ID), 3))
	q := rows.OrderBy(rows.DistinctOn(rows.FromModel(d.Query()), expr.Group(key)), expr.Asc(key), expr.Desc(id))
	s, err := rows.CompileRead(rows.Read(q))
	if err != nil || len(s.Arguments()) != 3 || !strings.Contains(s.SQL(), "DISTINCT ON (CASE") {
		t.Fatal(s.SQL(), s.Arguments(), err)
	}
	other := expr.When(expr.Gt(expr.OrderRow(id), 1), expr.Param(orm.KeyExpr(d.ID), 2)).Else(expr.Param(orm.KeyExpr(d.ID), 4))
	if _, err := rows.CompileRead(rows.Read(rows.OrderBy(rows.DistinctOn(rows.FromModel(d.Query()), expr.Group(key)), expr.Asc(other)))); !errors.Is(err, fault.Invalid) {
		t.Fatal("distinct prefix ignored parameters", err)
	}
	window := expr.WindowFor(rows.ModelScope(d.Query())).PartitionBy(expr.Group(key)).OrderBy(expr.Asc(id))
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(base, expr.RowNumber(window)))); err != nil {
		t.Fatal(err)
	}
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(base, expr.RowNumber(window.PartitionBy(expr.Group(key)))))); !errors.Is(err, fault.Invalid) {
		t.Fatal("duplicate partition", err)
	}
	count := expr.Count[member]()
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(base, expr.RowNumber(expr.WindowFor(rows.ModelScope(d.Query())).PartitionByValues(expr.Key(expr.AsSelected(count))))))); err != nil {
		t.Fatal("aggregate partition failed to group", err)
	}
	if _, err := rows.CompileRead(rows.ReadScalar(rows.DistinctOnScalarValues(rows.SelectScalar(base, expr.AsSelected(expr.Row(id))), expr.Key(expr.AsSelected(count))))); !errors.Is(err, fault.Invalid) {
		t.Fatal("aggregate key failed grouping validation", err)
	}
	if _, err := rows.CompileRead(rows.ReadScalar(rows.DistinctOnScalarValues(rows.SelectScalar(base, expr.AsSelected(count)), expr.Key(expr.AsSelected(count))))); err != nil {
		t.Fatal("selected aggregate key rejected", err)
	}
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(base, expr.RowNumber(window.PartitionByValues(expr.Key(expr.RowNumber(window))))))); !errors.Is(err, fault.Invalid) {
		t.Fatal("nested partition window", err)
	}
}
func TestGroupedKeyReuseInsideDistinctAndEmptyMembership(t *testing.T) {
	d := declareMembers()
	id := expr.Value(orm.KeyExpr(d.ID))
	base := rows.From(d.Query())
	key := expr.When(expr.Gt(expr.OrderRow(id), 1), expr.Param(orm.KeyExpr(d.ID), 2)).Else(expr.Param(orm.KeyExpr(d.ID), 3))
	outer := expr.AsSelected(expr.NullIf(key, expr.Param(orm.KeyExpr(d.ID), 4)))
	q := rows.OrderScalar(rows.DistinctOnScalarValues(rows.GroupByScalar(rows.SelectScalar(base, outer), expr.Group(key)), expr.Key(outer)), expr.AscValue(outer))
	s, err := rows.CompileRead(rows.ReadScalar(q))
	if err != nil || len(s.Arguments()) != 4 {
		t.Fatal("rebound grouped child", s.SQL(), s.Arguments(), err)
	}
	constant := expr.Param(orm.KeyExpr(d.ID), 1)
	choice := expr.When(expr.In(constant), expr.Param(orm.KeyExpr(d.ID), 9)).Else(expr.Param(orm.KeyExpr(d.ID), 7))
	s, err = rows.CompileRead(rows.ReadScalar(rows.GroupByScalar(rows.SelectScalar(base, expr.AsSelected(choice)), expr.Group(constant))))
	if err != nil || !reflect.DeepEqual(s.Arguments(), []any{int64(9), int64(7), int64(1)}) || !strings.Contains(s.SQL(), "GROUP BY CAST($3 AS bigint)") {
		t.Fatal("discarded operand left parameter cache", s.SQL(), s.Arguments(), err)
	}
	if _, err := rows.CompileRead(rows.ReadScalar(rows.OrderScalar(rows.DistinctScalar(rows.GroupByScalar(rows.SelectScalar(base, outer), expr.Group(key))), expr.AscValue(outer)))); err != nil {
		t.Fatal("ordinary DISTINCT grouped parameters", err)
	}
}
