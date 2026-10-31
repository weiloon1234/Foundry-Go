package rows_test

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

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
	c := rows.Correlate(rows.From(m.Query()), rows.From(o.Query()))
	parent := expr.At(rows.OuterScope(c, rows.ModelScope(m.Query())), orm.KeyExpr(m.ID))
	child := expr.At(rows.InnerScope(c, rows.ModelScope(o.Query())), orm.NumberExpr(o.MemberID))
	other := rows.Correlate(rows.From(m.Query()), rows.From(foreign.Query()))
	foreignChild := expr.At(rows.InnerScope(other, rows.ModelScope(foreign.Query())), orm.NumberExpr(foreign.MemberID))
	count := expr.Count[rows.Correlation[member, order]]()
	base := rows.SelectCorrelatedScalar(c.Where(expr.Equal(child, parent)), expr.AsSelected(count))
	grouped := rows.GroupByCorrelatedScalar(base, expr.Group(child))
	selected := rows.OrderCorrelatedScalar(rows.DistinctOnCorrelatedScalarValues(grouped, expr.Key(expr.AsSelected(count))), expr.DescValue(count)).Limit(1)
	compile := func(q rows.CorrelatedScalar[member, order, int64]) error {
		_, err := orm.Compile(expr.Where(m.Query(), q.Exists()))
		return err
	}
	if err := compile(selected); err != nil {
		t.Fatal(err)
	}
	if len(rows.CorrelatedScalarCore(&base).Source.Node.GroupBy) != 0 || rows.CorrelatedScalarCore(&grouped).Source.Node.Distinct.Kind != 0 || len(rows.CorrelatedScalarCore(&grouped).Source.Node.Orders) != 0 || rows.CorrelatedScalarCore(&grouped).Source.Node.Limit.IsSet() {
		t.Fatal("correlated clauses changed their input")
	}
	invalid := map[string]rows.CorrelatedScalar[member, order, int64]{
		"foreign group":       rows.GroupByCorrelatedScalar(base, expr.Group(foreignChild)),
		"foreign parameter":   rows.GroupByCorrelatedScalar(base, expr.Group(expr.ParamOf(expr.AsSelected(expr.Row(foreignChild)), 1))),
		"foreign key":         rows.DistinctOnCorrelatedScalarValues(grouped, expr.Key(expr.AsSelected(expr.Row(foreignChild)))),
		"foreign order":       rows.OrderCorrelatedScalar(grouped, promotedSelectedClauseOrder[rows.Correlation[member, order]]{expr.AscValue(expr.AsSelected(expr.Row(foreignChild)))}),
		"zero group":          rows.GroupByCorrelatedScalar(base, expr.GroupKey[rows.Correlation[member, order]]{}),
		"zero key":            rows.DistinctOnCorrelatedScalarValues(grouped, expr.ProjectionKey[rows.Correlation[member, order]]{}),
		"no key":              rows.DistinctOnCorrelatedScalarValues(grouped),
		"nil order":           rows.OrderCorrelatedScalar(grouped, nil),
		"zero order":          rows.OrderCorrelatedScalar(grouped, expr.SelectedOrder[rows.Correlation[member, order]]{}),
		"wrong prefix":        rows.OrderCorrelatedScalar(rows.DistinctOnCorrelatedScalarValues(grouped, expr.Key(expr.AsSelected(count))), expr.Asc(child)),
		"ungrouped selection": rows.DistinctOnCorrelatedScalarValues(rows.SelectCorrelatedScalar(c, expr.AsSelected(expr.Row(child))), expr.Key(expr.AsSelected(count))),
		"zero source":         rows.GroupByCorrelatedScalar(rows.CorrelatedScalar[member, order, int64]{}, expr.Group(child)),
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
	input := rows.Values(rows.SelectScalar(rows.From(m.Query()), expr.AsSelected(expr.Row(expr.Value(orm.NullableTextExpr(m.Nickname))))))
	base := rows.UnionAllValues(input, input)
	key := expr.AsSelected(expr.Row(base.Value()))
	selected := rows.DistinctOnValueSet(base, expr.Key(key)).OrderBy(expr.AscValue(key).NullsLast()).Offset(1).Limit(2)
	if rows.ValueSetQueryCore(base).Source.Node.Distinct.Kind != 0 || len(rows.ValueSetQueryCore(base).Source.Node.Orders) != 0 || rows.ValueSetQueryCore(base).Source.Node.Offset != 0 || rows.ValueSetQueryCore(base).Source.Node.Limit.IsSet() {
		t.Fatal("value-set clauses changed their input")
	}
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{"name"}, {nil}}}
	got, err := rows.ReadValues(selected).All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 2 || got[0] != value.Of("name") || !got[1].IsNull() {
		t.Fatal("nullable codec changed", got, err)
	}
	otherInput := rows.Values(rows.SelectScalar(rows.From(m.Query()), expr.AsSelected(expr.Row(expr.Value(orm.NullableTextExpr(m.Nickname))))))
	other := rows.UnionAllValues(otherInput, otherInput)
	for name, q := range map[string]rows.ValueSetQuery[value.Nullable[string]]{
		"foreign key":         rows.DistinctOnValueSet(base, expr.Key(expr.AsSelected(expr.Row(other.Value())))),
		"foreign parameter":   rows.DistinctOnValueSet(base, expr.Key(expr.AsSelected(expr.Row(expr.ParamOf(other.Value(), value.Of("foreign")))))),
		"zero key":            rows.DistinctOnValueSet(base, expr.ProjectionKey[rows.Set[value.Nullable[string]]]{}),
		"no key":              rows.DistinctOnValueSet(base),
		"wrong prefix":        rows.DistinctOnValueSet(base, expr.Key(key)).OrderBy(expr.AscValue(expr.AsSelected(expr.Row(expr.ParamOf(base.Value(), value.Of("constant")))))),
		"ungrouped aggregate": rows.DistinctOnValueSet(base, expr.Key(expr.AsSelected(expr.Count[rows.Set[value.Nullable[string]]]()))),
		"zero source":         rows.DistinctOnValueSet(rows.ValueSetQuery[value.Nullable[string]]{}, expr.Key(key)),
	} {
		t.Run(name, func(t *testing.T) {
			state := &driverState{}
			if got, err := rows.ReadValues(q).All(t.Context(), openTest(t, state)); !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
				t.Fatal("invalid value-set key reached execution", got, err)
			}
		})
	}
}
