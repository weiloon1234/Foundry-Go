package rows

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
	key := expr.When(expr.OrderRow(id).Gt(1), expr.Param(orm.KeyExpr(d.ID), 2)).Else(expr.Param(orm.KeyExpr(d.ID), 3))
	q := OrderScalar(HavingScalar(GroupByScalar(SelectScalar(From(d.Query()), key.Value()), expr.Group(key)), expr.Grouped(key.Ne(0))), key.Desc())
	s, err := CompileRead(ReadScalar(q))
	fragment := `CASE WHEN ("members"."id" > $1) THEN CAST($2 AS bigint) ELSE CAST($3 AS bigint) END`
	if err != nil || strings.Count(s.SQL(), fragment) != 4 || !reflect.DeepEqual(s.Arguments(), []any{int64(1), int64(2), int64(3), int64(0)}) {
		t.Fatal("group lost parameter identity", s.SQL(), s.Arguments(), err)
	}
	if _, err := CompileRead(ReadScalar(GroupByScalar(SelectScalar(From(d.Query()), expr.Row(id).Value()), expr.Group(key)))); !errors.Is(err, fault.Invalid) {
		t.Fatal("computed group selected ungrouped input", err)
	}
	equal := expr.When(expr.OrderRow(id).Gt(1), expr.Param(orm.KeyExpr(d.ID), 2)).Else(expr.Param(orm.KeyExpr(d.ID), 3))
	different := expr.When(expr.OrderRow(id).Gt(1), expr.Param(orm.KeyExpr(d.ID), 2)).Else(expr.Param(orm.KeyExpr(d.ID), 4))
	if _, err := CompileRead(ReadScalar(GroupByScalar(SelectScalar(From(d.Query()), equal.Value()), expr.Group(key)))); err != nil {
		t.Fatal("equivalent declarations differ", err)
	}
	if _, err := CompileRead(ReadScalar(GroupByScalar(SelectScalar(From(d.Query()), different.Value()), expr.Group(key)))); !errors.Is(err, fault.Invalid) {
		t.Fatal("group ignored parameters", err)
	}
	if _, err := CompileRead(ReadScalar(GroupByScalar(q, expr.Group(equal)))); !errors.Is(err, fault.Invalid) {
		t.Fatal("duplicate group", err)
	}
	outer := expr.NullIf(key, expr.Param(orm.KeyExpr(d.ID), 2))
	if _, err := CompileRead(ReadScalar(GroupByScalar(SelectScalar(From(d.Query()), outer.Value()), expr.Group(key)))); err != nil {
		t.Fatal("grouped subexpression missed", err)
	}
}
func TestComputedDistinctAndPartitionKeys(t *testing.T) {
	d := declareMembers()
	id := expr.Value(orm.KeyExpr(d.ID))
	base := From(d.Query())
	key := expr.When(expr.OrderRow(id).Gt(1), expr.Param(orm.KeyExpr(d.ID), 2)).Else(expr.Param(orm.KeyExpr(d.ID), 3))
	q := OrderBy(DistinctOn(FromModel(d.Query()), expr.Group(key)), key.Asc(), id.Desc())
	s, err := CompileRead(Read(q))
	if err != nil || len(s.Arguments()) != 3 || !strings.Contains(s.SQL(), "DISTINCT ON (CASE") {
		t.Fatal(s.SQL(), s.Arguments(), err)
	}
	other := expr.When(expr.OrderRow(id).Gt(1), expr.Param(orm.KeyExpr(d.ID), 2)).Else(expr.Param(orm.KeyExpr(d.ID), 4))
	if _, err := CompileRead(Read(OrderBy(DistinctOn(FromModel(d.Query()), expr.Group(key)), other.Asc()))); !errors.Is(err, fault.Invalid) {
		t.Fatal("distinct prefix ignored parameters", err)
	}
	window := expr.WindowFor(ModelScope(d.Query())).PartitionBy(expr.Group(key)).OrderBy(id.Asc())
	if _, err := CompileRead(ReadScalar(SelectScalar(base, expr.RowNumber(window)))); err != nil {
		t.Fatal(err)
	}
	if _, err := CompileRead(ReadScalar(SelectScalar(base, expr.RowNumber(window.PartitionBy(expr.Group(key)))))); !errors.Is(err, fault.Invalid) {
		t.Fatal("duplicate partition", err)
	}
	count := expr.Count[member]()
	if _, err := CompileRead(ReadScalar(SelectScalar(base, expr.RowNumber(expr.WindowFor(ModelScope(d.Query())).PartitionByValues(count.Value().Key()))))); err != nil {
		t.Fatal("aggregate partition failed to group", err)
	}
	if _, err := CompileRead(ReadScalar(DistinctOnScalarValues(SelectScalar(base, expr.Row(id).Value()), count.Value().Key()))); !errors.Is(err, fault.Invalid) {
		t.Fatal("aggregate key failed grouping validation", err)
	}
	if _, err := CompileRead(ReadScalar(DistinctOnScalarValues(SelectScalar(base, count.Value()), count.Value().Key()))); err != nil {
		t.Fatal("selected aggregate key rejected", err)
	}
	if _, err := CompileRead(ReadScalar(SelectScalar(base, expr.RowNumber(window.PartitionByValues(expr.RowNumber(window).Key()))))); !errors.Is(err, fault.Invalid) {
		t.Fatal("nested partition window", err)
	}
}
func TestGroupedKeyReuseInsideDistinctAndEmptyMembership(t *testing.T) {
	d := declareMembers()
	id := expr.Value(orm.KeyExpr(d.ID))
	base := From(d.Query())
	key := expr.When(expr.OrderRow(id).Gt(1), expr.Param(orm.KeyExpr(d.ID), 2)).Else(expr.Param(orm.KeyExpr(d.ID), 3))
	outer := expr.NullIf(key, expr.Param(orm.KeyExpr(d.ID), 4)).Value()
	q := OrderScalar(DistinctOnScalarValues(GroupByScalar(SelectScalar(base, outer), expr.Group(key)), outer.Key()), outer.Asc())
	s, err := CompileRead(ReadScalar(q))
	if err != nil || len(s.Arguments()) != 4 {
		t.Fatal("rebound grouped child", s.SQL(), s.Arguments(), err)
	}
	constant := expr.Param(orm.KeyExpr(d.ID), 1)
	choice := expr.When(constant.In(), expr.Param(orm.KeyExpr(d.ID), 9)).Else(expr.Param(orm.KeyExpr(d.ID), 7))
	s, err = CompileRead(ReadScalar(GroupByScalar(SelectScalar(base, choice.Value()), expr.Group(constant))))
	if err != nil || !reflect.DeepEqual(s.Arguments(), []any{int64(9), int64(7), int64(1)}) || !strings.Contains(s.SQL(), "GROUP BY CAST($3 AS bigint)") {
		t.Fatal("discarded operand left parameter cache", s.SQL(), s.Arguments(), err)
	}
	if _, err := CompileRead(ReadScalar(OrderScalar(DistinctScalar(GroupByScalar(SelectScalar(base, outer), expr.Group(key))), outer.Asc()))); err != nil {
		t.Fatal("ordinary DISTINCT grouped parameters", err)
	}
}
