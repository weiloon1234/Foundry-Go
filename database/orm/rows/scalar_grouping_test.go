package rows

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
	base := SelectScalar(From(m.Query()), count.Value())
	compile := func(q Scalar[member, int64]) error {
		_, err := orm.Compile(expr.Where(m.Query(), expr.Equal(expr.Value(orm.KeyExpr(m.ID)), expr.Coalesce(ScalarRowQuery(From(m.Query()), q), expr.Param(orm.KeyExpr(m.ID), 0)))))
		return err
	}
	grouped := GroupByScalar(base, expr.Group(expr.Value(orm.KeyExpr(m.ID))))
	selected := OrderScalar(DistinctScalar(HavingScalar(grouped, count.Gt(0))), count.Desc())
	if err := compile(selected); err != nil {
		t.Fatal(err)
	}
	if len(base.source.Node.GroupBy) != 0 || len(base.source.Node.Having) != 0 || base.source.Node.Distinct.Kind != 0 {
		t.Fatal("scalar clauses changed their input")
	}
	if err := compile(OrderScalar(DistinctOnScalarValues(grouped, count.Value().Key()), count.Asc())); err != nil {
		t.Fatal(err)
	}
	if len(grouped.source.Node.Having) != 0 || grouped.source.Node.Distinct.Kind != 0 {
		t.Fatal("later scalar clauses changed their input")
	}
	id := SelectScalar(From(m.Query()), expr.Row(expr.Value(orm.KeyExpr(m.ID))).Value())
	for _, invalid := range []Scalar[member, int64]{
		GroupByScalar(base, expr.Group(expr.Value(orm.KeyExpr(foreign.ID)))),
		GroupByScalar(base, expr.Group(expr.Param(orm.KeyExpr(foreign.ID), 1))),
		HavingScalar(base, expr.CountField(orm.KeyExpr(foreign.ID)).Gt(0)),
		HavingScalar(base, expr.HavingPredicate[member]{}),
		DistinctOnScalar(id, expr.Group(expr.Value(orm.KeyExpr(foreign.ID)))),
		DistinctOnScalar(id, expr.GroupKey[member]{}),
		DistinctOnScalar(id),
		DistinctOnScalarValues(base),
		DistinctOnScalarValues(base, expr.CountField(orm.KeyExpr(foreign.ID)).Value().Key()),
		DistinctOnScalarValues(base, expr.ProjectionKey[member]{}),
		OrderScalar(DistinctOnScalar(id, expr.Group(expr.Value(orm.KeyExpr(m.ID)))), expr.Value(orm.TextExpr(m.Name)).Asc()),
		DistinctScalar(Scalar[member, int64]{}),
	} {
		if err := compile(invalid); !errors.Is(err, fault.Invalid) {
			t.Fatalf("invalid scalar clause accepted: %v", err)
		}
	}
	// DISTINCT replaces the key list, so later ordering need not keep its prefix.
	if err := compile(DistinctScalar(DistinctOnScalar(id, expr.Group(expr.Value(orm.TextExpr(m.Name)))))); err != nil {
		t.Fatal(err)
	}
}

func TestDistinctRecordsAndSetsKeepInputsAndRejectZero(t *testing.T) {
	m := declareMembers()
	input := SelectRecord(From(m.Query()), ModelScope(m.Query()))
	record := DistinctRecord(input)
	set := UnionAll(input, input)
	distinctSet := DistinctSet(set)
	scalar := Values(SelectScalar(From(m.Query()), expr.Row(expr.Value(orm.KeyExpr(m.ID))).Value()))
	values := UnionAllValues(scalar, scalar)
	distinctValues := DistinctValues(values)
	if input.core.Source.Node.Distinct.Kind != 0 || set.core.Source.Node.Distinct.Kind != 0 || values.core.Source.Node.Distinct.Kind != 0 {
		t.Fatal("distinct changed its input")
	}
	for _, source := range []RecordSource[member]{record, distinctSet} {
		alias := AsRecord[left](source, "unique_members")
		if _, err := orm.Compile(expr.Where(m.Query(), ExistsQuery(From(m.Query()), alias))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := orm.Compile(expr.Where(m.Query(), ExistsQuery(From(m.Query()), distinctValues))); err != nil {
		t.Fatal(err)
	}
	for _, source := range []RecordSource[member]{DistinctRecord(RecordRead[member, member]{}), DistinctSet(SetQuery[member]{})} {
		alias := AsRecord[left](source, "invalid")
		if _, err := orm.Compile(expr.Where(m.Query(), ExistsQuery(From(m.Query()), alias))); !errors.Is(err, fault.Invalid) {
			t.Fatalf("zero distinct record accepted: %v", err)
		}
	}
	if _, err := orm.Compile(expr.Where(m.Query(), ExistsQuery(From(m.Query()), DistinctValues(ValueSetQuery[int64]{})))); !errors.Is(err, fault.Invalid) {
		t.Fatalf("zero distinct values accepted: %v", err)
	}
}
