package rows

import (
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type promotedSelectedClauseOrder[S any] struct{ expr.SelectedOrder[S] }

func TestCorrelatedSelectedKeysKeepOwnershipPhaseAndOrdering(t *testing.T) {
	m, o, foreign := declareMembers(), declareOrders(), declareOrders()
	c := Correlate(From(m.Query()), From(o.Query()))
	parent := expr.At(OuterScope(c, ModelScope(m.Query())), orm.KeyExpr(m.ID))
	child := expr.At(InnerScope(c, ModelScope(o.Query())), orm.NumberExpr(o.MemberID))
	other := Correlate(From(m.Query()), From(foreign.Query()))
	foreignChild := expr.At(InnerScope(other, ModelScope(foreign.Query())), orm.NumberExpr(foreign.MemberID))
	count := expr.Count[Correlation[member, order]]()
	base := SelectCorrelatedScalar(c.Where(expr.Equal(child, parent)), count.Value())
	grouped := GroupByCorrelatedScalar(base, expr.Group(child))
	selected := OrderCorrelatedScalar(DistinctOnCorrelatedScalarValues(grouped, count.Value().Key()), count.Desc()).Limit(1)
	compile := func(q CorrelatedScalar[member, order, int64]) error {
		_, err := orm.Compile(expr.Where(m.Query(), q.Exists()))
		return err
	}
	if err := compile(selected); err != nil {
		t.Fatal(err)
	}
	if len(base.core.Source.Node.GroupBy) != 0 || grouped.core.Source.Node.Distinct.Kind != 0 || len(grouped.core.Source.Node.Orders) != 0 || grouped.core.Source.Node.Limit.IsSet() {
		t.Fatal("correlated clauses changed their input")
	}
	invalid := map[string]CorrelatedScalar[member, order, int64]{
		"foreign group":       GroupByCorrelatedScalar(base, expr.Group(foreignChild)),
		"foreign parameter":   GroupByCorrelatedScalar(base, expr.Group(expr.Row(foreignChild).Value().Param(1))),
		"foreign key":         DistinctOnCorrelatedScalarValues(grouped, expr.Row(foreignChild).Value().Key()),
		"foreign order":       OrderCorrelatedScalar(grouped, promotedSelectedClauseOrder[Correlation[member, order]]{expr.Row(foreignChild).Value().Asc()}),
		"zero group":          GroupByCorrelatedScalar(base, expr.GroupKey[Correlation[member, order]]{}),
		"zero key":            DistinctOnCorrelatedScalarValues(grouped, expr.ProjectionKey[Correlation[member, order]]{}),
		"no key":              DistinctOnCorrelatedScalarValues(grouped),
		"nil order":           OrderCorrelatedScalar(grouped, nil),
		"zero order":          OrderCorrelatedScalar(grouped, expr.SelectedOrder[Correlation[member, order]]{}),
		"wrong prefix":        OrderCorrelatedScalar(DistinctOnCorrelatedScalarValues(grouped, count.Value().Key()), child.Asc()),
		"ungrouped selection": DistinctOnCorrelatedScalarValues(SelectCorrelatedScalar(c, expr.Row(child).Value()), count.Value().Key()),
		"zero source":         GroupByCorrelatedScalar(CorrelatedScalar[member, order, int64]{}, expr.Group(child)),
	}
	for name, q := range invalid {
		t.Run(name, func(t *testing.T) {
			if err := compile(q); !errors.Is(err, fault.Invalid) {
				t.Fatalf("invalid clause accepted: %v", err)
			}
		})
	}
	// An inner scope remains bound to its original enclosing declaration.
	otherParent := declareMembers()
	if _, err := orm.Compile(expr.Where(otherParent.Query(), selected.Exists())); !errors.Is(err, fault.Invalid) {
		t.Fatal("clause adapters erased correlation ownership", err)
	}
}

func TestValueSetSelectedKeysKeepCodecInputsAndRejectForeignKeys(t *testing.T) {
	m := declareMembers()
	input := Values(SelectScalar(From(m.Query()), expr.Row(expr.Value(orm.NullableTextExpr(m.Nickname))).Value()))
	base := UnionAllValues(input, input)
	key := expr.Row(base.Value()).Value()
	selected := DistinctOnValueSet(base, key.Key()).OrderBy(key.Asc().NullsLast()).Offset(1).Limit(2)
	if base.core.Source.Node.Distinct.Kind != 0 || len(base.core.Source.Node.Orders) != 0 || base.core.Source.Node.Offset != 0 || base.core.Source.Node.Limit.IsSet() {
		t.Fatal("value-set clauses changed their input")
	}
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{"name"}, {nil}}}
	got, err := ReadValues(selected).All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 2 || got[0] != value.Of("name") || !got[1].IsNull() {
		t.Fatal("nullable codec changed", got, err)
	}
	otherInput := Values(SelectScalar(From(m.Query()), expr.Row(expr.Value(orm.NullableTextExpr(m.Nickname))).Value()))
	other := UnionAllValues(otherInput, otherInput)
	for name, q := range map[string]ValueSetQuery[value.Nullable[string]]{
		"foreign key":         DistinctOnValueSet(base, expr.Row(other.Value()).Value().Key()),
		"foreign parameter":   DistinctOnValueSet(base, expr.Row(other.Value().Param(value.Of("foreign"))).Value().Key()),
		"zero key":            DistinctOnValueSet(base, expr.ProjectionKey[Set[value.Nullable[string]]]{}),
		"no key":              DistinctOnValueSet(base),
		"wrong prefix":        DistinctOnValueSet(base, key.Key()).OrderBy(expr.Row(base.Value().Param(value.Of("constant"))).Value().Asc()),
		"ungrouped aggregate": DistinctOnValueSet(base, expr.Count[Set[value.Nullable[string]]]().Value().Key()),
		"zero source":         DistinctOnValueSet(ValueSetQuery[value.Nullable[string]]{}, key.Key()),
	} {
		t.Run(name, func(t *testing.T) {
			state := &driverState{}
			if got, err := ReadValues(q).All(t.Context(), openTest(t, state)); !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
				t.Fatal("invalid value-set key reached execution", got, err)
			}
		})
	}
}
