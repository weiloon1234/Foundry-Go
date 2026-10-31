package rows_test

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestScalarGroupingPreservesOwnershipAndInput(t *testing.T) {
	m, foreign := declareMembers(), declareMembers()
	count := expr.Count[member]()
	base := rows.SelectScalar(rows.From(m.Query()), expr.AsSelected(count))
	compile := func(q rows.Scalar[member, int64]) error {
		_, err := orm.Compile(expr.Where(m.Query(), expr.Equal(expr.Value(orm.KeyExpr(m.ID)), expr.Coalesce(rows.ScalarRowQuery(rows.From(m.Query()), q), expr.Param(orm.KeyExpr(m.ID), 0)))))
		return err
	}
	grouped := rows.GroupByScalar(base, expr.Group(expr.Value(orm.KeyExpr(m.ID))))
	selected := rows.OrderScalar(rows.DistinctScalar(rows.HavingScalar(grouped, expr.GtValue(count, 0))), expr.DescValue(count))
	if err := compile(selected); err != nil {
		t.Fatal(err)
	}
	if len(rows.SourceOf(base).Node.GroupBy) != 0 || len(rows.SourceOf(base).Node.Having) != 0 || rows.SourceOf(base).Node.Distinct.Kind != 0 {
		t.Fatal("scalar clauses changed their input")
	}
	if err := compile(rows.OrderScalar(rows.DistinctOnScalarValues(grouped, expr.Key(expr.AsSelected(count))), expr.AscValue(count))); err != nil {
		t.Fatal(err)
	}
	if len(rows.SourceOf(grouped).Node.Having) != 0 || rows.SourceOf(grouped).Node.Distinct.Kind != 0 {
		t.Fatal("later scalar clauses changed their input")
	}
	id := rows.SelectScalar(rows.From(m.Query()), expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(m.ID)))))
	for _, invalid := range []rows.Scalar[member, int64]{
		rows.GroupByScalar(base, expr.Group(expr.Value(orm.KeyExpr(foreign.ID)))),
		rows.GroupByScalar(base, expr.Group(expr.Param(orm.KeyExpr(foreign.ID), 1))),
		rows.HavingScalar(base, expr.GtValue(expr.CountField(orm.KeyExpr(foreign.ID)), 0)),
		rows.HavingScalar(base, expr.HavingPredicate[member]{}),
		rows.DistinctOnScalar(id, expr.Group(expr.Value(orm.KeyExpr(foreign.ID)))),
		rows.DistinctOnScalar(id, expr.GroupKey[member]{}),
		rows.DistinctOnScalar(id),
		rows.DistinctOnScalarValues(base),
		rows.DistinctOnScalarValues(base, expr.Key(expr.AsSelected(expr.CountField(orm.KeyExpr(foreign.ID))))),
		rows.DistinctOnScalarValues(base, expr.ProjectionKey[member]{}),
		rows.OrderScalar(rows.DistinctOnScalar(id, expr.Group(expr.Value(orm.KeyExpr(m.ID)))), expr.Asc(expr.Value(orm.TextExpr(m.Name)))),
		rows.DistinctScalar(rows.Scalar[member, int64]{}),
	} {
		if err := compile(invalid); !errors.Is(err, fault.Invalid) {
			t.Fatalf("invalid scalar clause accepted: %v", err)
		}
	}
	// DISTINCT replaces the key list, so later ordering need not keep its prefix.
	if err := compile(rows.DistinctScalar(rows.DistinctOnScalar(id, expr.Group(expr.Value(orm.TextExpr(m.Name)))))); err != nil {
		t.Fatal(err)
	}
}

func TestDistinctRecordsAndSetsKeepInputsAndRejectZero(t *testing.T) {
	m := declareMembers()
	input := rows.SelectRecord(rows.From(m.Query()), rows.ModelScope(m.Query()))
	record := rows.DistinctRecord(input)
	set := rows.UnionAll(input, input)
	distinctSet := rows.DistinctSet(set)
	scalar := rows.Values(rows.SelectScalar(rows.From(m.Query()), expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(m.ID))))))
	values := rows.UnionAllValues(scalar, scalar)
	distinctValues := rows.DistinctValues(values)
	if rows.RecordReadCore(input).Source.Node.Distinct.Kind != 0 || rows.SetQueryCore(set).Source.Node.Distinct.Kind != 0 || rows.ValueSetQueryCore(values).Source.Node.Distinct.Kind != 0 {
		t.Fatal("distinct changed its input")
	}
	for _, source := range []rows.RecordSource[member]{record, distinctSet} {
		alias := rows.AsRecord[left](source, "unique_members")
		if _, err := orm.Compile(expr.Where(m.Query(), rows.ExistsQuery(rows.From(m.Query()), alias))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := orm.Compile(expr.Where(m.Query(), rows.ExistsQuery(rows.From(m.Query()), distinctValues))); err != nil {
		t.Fatal(err)
	}
	for _, source := range []rows.RecordSource[member]{rows.DistinctRecord(rows.RecordRead[member, member]{}), rows.DistinctSet(rows.SetQuery[member]{})} {
		alias := rows.AsRecord[left](source, "invalid")
		if _, err := orm.Compile(expr.Where(m.Query(), rows.ExistsQuery(rows.From(m.Query()), alias))); !errors.Is(err, fault.Invalid) {
			t.Fatalf("zero distinct record accepted: %v", err)
		}
	}
	if _, err := orm.Compile(expr.Where(m.Query(), rows.ExistsQuery(rows.From(m.Query()), rows.DistinctValues(rows.ValueSetQuery[int64]{})))); !errors.Is(err, fault.Invalid) {
		t.Fatalf("zero distinct values accepted: %v", err)
	}
}
