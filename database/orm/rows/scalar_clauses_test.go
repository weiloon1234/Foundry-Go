package rows

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type promotedClauseOrder[S any] struct{ expr.Order[S] }

func TestScalarClausesPreserveSourceAndRejectForeignOrders(t *testing.T) {
	m := declareMembers()
	a := AsRecord[left](CTE("eligible", ModelRecord(m.Query())), "chosen")
	field := expr.At(Scope(a), orm.KeyExpr(m.ID))
	base := SelectScalar(a, expr.AsSelected(expr.Row(field)))
	selected := LimitScalar(OffsetScalar(OrderScalar(base, expr.Desc(field)), 2), 1)
	if base.source.Node.Limit.IsSet() || base.source.Node.Offset != 0 || len(base.source.Node.Orders) != 0 {
		t.Fatal("scalar clauses mutated their input")
	}
	compile := func(q Scalar[Alias[left, member], int64]) error {
		_, err := orm.Compile(expr.Where(m.Query(), expr.Equal(expr.Value(orm.KeyExpr(m.ID)), expr.Coalesce(ScalarRowQuery(From(m.Query()), q), expr.Param(orm.KeyExpr(m.ID), 0)))))
		return err
	}
	if err := compile(selected); err != nil {
		t.Fatal(err)
	}
	other := declareMembers()
	foreign := AsRecord[left](CTE("eligible", ModelRecord(other.Query())), "chosen")
	for _, q := range []Scalar[Alias[left, member], int64]{
		OrderScalar(base, promotedClauseOrder[Alias[left, member]]{expr.Asc(expr.At(Scope(foreign), orm.KeyExpr(other.ID)))}),
		OrderScalar(base, nil), LimitScalar(base, -1), OffsetScalar(base, -1),
		LimitScalar(Scalar[Alias[left, member], int64]{}, 1),
	} {
		if err := compile(q); !errors.Is(err, fault.Invalid) {
			t.Fatalf("invalid scalar source accepted: %v", err)
		}
	}
}

func TestCorrelatedClausesKeepCapabilitiesAndErrors(t *testing.T) {
	m := declareMembers()
	a, b := As[left](m.Query(), "a"), As[right](m.Query(), "b")
	c := Correlate(a, b)
	parent := expr.At(OuterScope(c, Scope(a)), orm.KeyExpr(m.ID))
	child := expr.At(InnerScope(c, Scope(b)), orm.KeyExpr(m.ID))
	scalar := SelectCorrelatedScalar(c.Where(expr.Less(child, parent)), expr.AsSelected(expr.Row(child)))
	distinct := DistinctOnCorrelatedScalar(scalar, expr.Group(parent)).OrderBy(expr.Asc(parent), expr.Desc(child))
	if scalar.core.Source.Node.Distinct.Kind != 0 {
		t.Fatal("distinct changed the input")
	}
	if _, err := orm.Compile(expr.Where(m.Query(), ExistsQuery(From(m.Query()), SelectScalar(a, expr.AsSelected(CorrelatedScalarRowQuery(distinct)))))); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []CorrelatedScalar[Alias[left, member], Alias[right, member], int64]{
		DistinctOnCorrelatedScalar(scalar),
		DistinctOnCorrelatedScalar(scalar, expr.Group(parent)).OrderBy(expr.Asc(child)),
	} {
		_, err := orm.Compile(expr.Where(m.Query(), ExistsQuery(From(m.Query()), SelectScalar(a, expr.AsSelected(CorrelatedScalarRowQuery(invalid))))))
		if !errors.Is(err, fault.Invalid) {
			t.Fatalf("invalid correlated distinct clause accepted: %v", err)
		}
	}
	record := SelectCorrelatedRecord(c, InnerScope(c, Scope(b)))
	if record.core.Correlation.Source.Node.Distinct.Kind != 0 {
		t.Fatal("unexpected initial distinct")
	}
	changed := DistinctCorrelatedRecord(record)
	if record.core.Correlation.Source.Node.Distinct.Kind != 0 || changed.core.Correlation.Source.Node.Distinct.Kind == 0 {
		t.Fatal("distinct did not preserve its input")
	}
	if !errors.Is(HavingCorrelatedRecord(record, expr.HavingPredicate[Correlation[Alias[left, member], Alias[right, member]]]{}).core.Correlation.Source.Err, fault.Invalid) {
		t.Fatal("zero HAVING predicate accepted")
	}
	if !errors.Is(DistinctOnCorrelatedScalar(scalar, expr.GroupKey[Correlation[Alias[left, member], Alias[right, member]]]{}).core.Source.Err, fault.Invalid) {
		t.Fatal("zero distinct key accepted")
	}
}
