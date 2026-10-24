package goway_test

import (
	"database/sql/driver"
	"fmt"
	"testing"

	"foundry.test/goway/models"
	old "foundry.test/goway/oldmodels"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/value"
)

type windowGoldenRow[V any] struct{ Value V }
type windowGoldenFields[V any] struct {
	rowset.Row[windowGoldenRow[V]]
	Value rowset.Field[windowGoldenRow[V], V]
}

func windowGolden[V any](t *testing.T, oldValue query.Expression[query.Alias[expressionSide, old.Order], V], newValue expr.Selected[rowset.Alias[expressionSide, models.Order], V], c codec.Codec[V], sample driver.Value) {
	t.Helper()
	before, after := &goldenCapture{}, &goldenCapture{}
	for _, g := range []*goldenCapture{before, after} {
		g.scriptedColumns = []string{"value"}
		g.scriptedBatches = [][][]driver.Value{{{sample}}}
	}
	ctx := t.Context()
	oldDB, newDB := before.open(ctx), after.open(ctx)
	defer oldDB.Close(ctx)
	defer newDB.Close(ctx)
	oldRows := must(query.SelectValue(query.As[expressionSide](old.QueryOrders(), "o"), oldValue).All(ctx, oldDB))
	fields := rowset.Define(func(b rowset.Builder[windowGoldenRow[V]], r *windowGoldenRow[V]) windowGoldenFields[V] {
		return windowGoldenFields[V]{b.Row(), rowset.FieldOf(b, &r.Value, orm.Codec(c))}
	})
	got := must(rowset.Select(rowset.As[expressionSide](models.Orders.Query(), "o"), fields, rowset.MapSelected(fields.Value, newValue)).All(ctx, newDB))
	normalized := make([]V, len(got))
	for i, r := range got {
		normalized[i] = r.Value
	}
	fmt.Fprintln(&before.output, oldRows)
	fmt.Fprintln(&after.output, normalized)
	if before.statements != 1 || after.statements != 1 || before.output.String() != after.output.String() {
		t.Fatalf("window SQL/bindings/hydration parity:\n%s", goldenDiff(before.output.String(), after.output.String()))
	}
}
func TestStage6d1WindowGolden(t *testing.T) {
	oa := query.As[expressionSide](old.QueryOrders(), "o")
	of := old.OrderFieldsAt(oa.Scope())
	na := rowset.As[expressionSide](models.Orders.Query(), "o")
	ns := rowset.Scope(na)
	nv := expr.At(ns, models.Orders.TotalCents)
	ow := query.WindowFor(oa).PartitionBy(of.BuyerID.Group()).OrderBy(of.TotalCents.Desc()).RowsBetween(query.Preceding(2), query.CurrentRow()).ExcludeTies()
	nw := expr.WindowFor(ns).PartitionBy(expr.Group(expr.At(ns, models.Orders.BuyerID))).OrderBy(nv.Desc()).RowsBetween(expr.Preceding(2), expr.CurrentRow()).ExcludeTies()
	windowGolden(t, query.RowNumber(ow), expr.RowNumber(nw), codec.Signed[int64](), int64(1))
	windowGolden(t, query.Rank(ow), expr.Rank(nw), codec.Signed[int64](), int64(1))
	windowGolden(t, query.DenseRank(ow), expr.DenseRank(nw), codec.Signed[int64](), int64(1))
	windowGolden(t, query.PercentRank(ow), expr.PercentRank(nw), codec.Float[float64](), float64(0))
	windowGolden(t, query.CumeDist(ow), expr.CumeDist(nw), codec.Float[float64](), float64(1))
	windowGolden(t, query.NTile(2, ow), expr.NTile(2, nw), codec.Signed[int32](), int64(1))
	windowGolden(t, query.LagOr(query.Value(of.TotalCents), 1, int64(99), ow), expr.LagOr(expr.Select(nv), 1, int64(99), nw), codec.Signed[int64](), int64(99))
	windowGolden(t, query.Lead(query.Value(of.TotalCents), 1, ow), expr.Lead(expr.Select(nv), 1, nw), codec.Nullable(codec.Signed[int64]()), nil)
	windowGolden(t, query.FirstValue(query.Value(of.TotalCents), ow), expr.FirstValue(expr.Select(nv), nw), codec.Nullable(codec.Signed[int64]()), int64(7))
	windowGolden(t, query.LastValue(query.Value(of.TotalCents), ow), expr.LastValue(expr.Select(nv), nw), codec.Nullable(codec.Signed[int64]()), int64(7))
	windowGolden(t, query.NthValue(query.Value(of.TotalCents), 2, ow), expr.NthValue(expr.Select(nv), 2, nw), codec.Nullable(codec.Signed[int64]()), nil)
	windowGolden(t, query.Count[query.Alias[expressionSide, old.Order]]().Filter(of.TotalCents.Gt(0)).Over(ow), expr.Over(expr.Count[rowset.Alias[expressionSide, models.Order]]().Filter(expr.OrderRow(nv).Gt(0)), nw), codec.Signed[int64](), int64(2))
	windowGolden(t, query.Sum(of.TotalCents).Over(ow), expr.Over(expr.SumAt(ns, models.Orders.TotalCents), nw), codec.Nullable(codec.Decimal()), "12")
	windowGolden(t, query.Avg(of.TotalCents).Over(ow), expr.Over(expr.AvgAt(ns, models.Orders.TotalCents), nw), codec.Nullable(codec.Decimal()), "6")
	windowGolden(t, query.Min(of.TotalCents).Over(ow), expr.Over(expr.MinAt(ns, models.Orders.TotalCents), nw), codec.Nullable(codec.Signed[int64]()), int64(5))
	windowGolden(t, query.Max(of.TotalCents).Over(ow), expr.Over(expr.MaxAt(ns, models.Orders.TotalCents), nw), codec.Nullable(codec.Signed[int64]()), int64(7))
	windowGolden(t, query.Exists[query.Alias[expressionSide, old.Order]]().Over(ow), expr.Over(expr.Exists[rowset.Alias[expressionSide, models.Order]](), nw), codec.Bool[bool](), true)
	windowGolden(t, query.CountField(of.TotalCents).Over(ow), expr.Over(expr.CountFieldAt(ns, models.Orders.TotalCents), nw), codec.Signed[int64](), int64(2))
	windowGolden(t, query.RowNumber(ow.Named("framed")), expr.RowNumber(nw.Named("framed")), codec.Signed[int64](), int64(1))
	ob := query.WindowFor(oa).OrderBy(of.TotalCents.Asc()).Named("base")
	nb := expr.WindowFor(ns).OrderBy(nv.Asc()).Named("base")
	windowGolden(t, query.RowNumber(ob.GroupsBetween(query.CurrentRow(), query.Following(1)).ExcludeGroup()), expr.RowNumber(nb.GroupsBetween(expr.CurrentRow(), expr.Following(1)).ExcludeGroup()), codec.Signed[int64](), int64(1))
	windowGolden(t, query.RowNumber(ob.RangeBetween(query.UnboundedPreceding(), query.UnboundedFollowing()).ExcludeCurrentRow()), expr.RowNumber(nb.RangeBetween(expr.UnboundedPreceding(), expr.UnboundedFollowing()).ExcludeCurrentRow()), codec.Signed[int64](), int64(1))
	or := query.NumericRange(query.WindowFor(oa), query.Value(of.TotalCents)).Desc()
	nr := expr.NumericRange(expr.WindowFor(ns), expr.Select(nv)).Desc()
	windowGolden(t, query.RowNumber(or.Between(or.Preceding(5), or.Following(10))), expr.RowNumber(nr.Between(nr.Preceding(5), nr.Following(10))), codec.Signed[int64](), int64(1))
	// Nullable helpers preserve one wrapper and the input codec.
	windowGolden(t, query.LagNullable(query.Nullable(query.Value(of.TotalCents)), 1, ow), expr.LagNullable(expr.Nullable(expr.Select(nv)), 1, nw), codec.Nullable(codec.Signed[int64]()), nil)
	windowGolden(t, query.LeadOr(query.Nullable(query.Value(of.TotalCents)), 1, value.Of(int64(1)), ow), expr.LeadOr(expr.Nullable(expr.Select(nv)), 1, value.Of(int64(1)), nw), codec.Nullable(codec.Signed[int64]()), int64(1))
}
