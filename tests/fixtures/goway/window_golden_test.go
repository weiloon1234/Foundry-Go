package goway_test

import (
	"database/sql/driver"
	"fmt"
	"testing"

	"foundry.test/goway/models"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"github.com/weiloon1234/Foundry-Go/value"
)

type windowGoldenRow[V any] struct{ Value V }
type windowGoldenFields[V any] struct {
	rowset.Row[windowGoldenRow[V]]
	Value rowset.Field[windowGoldenRow[V], V]
}

func windowGolden[V any](t *testing.T, newValue expr.Selected[rowset.Alias[expressionSide, models.Order], V], c codec.Codec[V], sample driver.Value) {
	t.Helper()
	after := &goldenCapture{}
	for _, g := range []*goldenCapture{after} {
		g.scriptedColumns = []string{"value"}
		g.scriptedBatches = [][][]driver.Value{{{sample}}}
	}
	ctx := t.Context()
	newDB := after.open(ctx)

	defer newDB.Close(ctx)

	fields := rowset.Define(func(b rowset.Builder[windowGoldenRow[V]], r *windowGoldenRow[V]) windowGoldenFields[V] {
		return windowGoldenFields[V]{b.Row(), rowset.FieldOf(b, &r.Value, orm.Codec(c))}
	})
	got := must(rowset.Select(rowset.As[expressionSide](models.Orders.Query(), "o"), fields, rowset.MapSelected(fields.Value, newValue)).All(ctx, newDB))
	normalized := make([]V, len(got))
	for i, r := range got {
		normalized[i] = r.Value
	}

	fmt.Fprintln(&after.output, oracleSnapshot([]any{normalized}))
	if after.statements != 1 || !oracleEqual(t, "window_golden/1", after.output.String()) {
		t.Fatal("frozen legacy comparison differs")
	}
}
func TestStage6d1WindowGolden(t *testing.T) {
	na := rowset.As[expressionSide](models.Orders.Query(), "o")
	ns := rowset.Scope(na)
	nv := expr.At(ns, models.Orders.TotalCents)

	nw := expr.WindowFor(ns).PartitionBy(expr.Group(expr.At(ns, models.Orders.BuyerID))).OrderBy(nv.Desc()).RowsBetween(expr.Preceding(2), expr.CurrentRow()).ExcludeTies()
	windowGolden(t, expr.RowNumber(nw), codec.Signed[int64](), int64(1))
	windowGolden(t, expr.Rank(nw), codec.Signed[int64](), int64(1))
	windowGolden(t, expr.DenseRank(nw), codec.Signed[int64](), int64(1))
	windowGolden(t, expr.PercentRank(nw), codec.Float[float64](), float64(0))
	windowGolden(t, expr.CumeDist(nw), codec.Float[float64](), float64(1))
	windowGolden(t, expr.NTile(2, nw), codec.Signed[int32](), int64(1))
	windowGolden(t, expr.LagOr(expr.Row(nv).Value(), 1, int64(99), nw), codec.Signed[int64](), int64(99))
	windowGolden(t, expr.Lead(expr.Row(nv).Value(), 1, nw), codec.Nullable(codec.Signed[int64]()), nil)
	windowGolden(t, expr.FirstValue(expr.Row(nv).Value(), nw), codec.Nullable(codec.Signed[int64]()), int64(7))
	windowGolden(t, expr.LastValue(expr.Row(nv).Value(), nw), codec.Nullable(codec.Signed[int64]()), int64(7))
	windowGolden(t, expr.NthValue(expr.Row(nv).Value(), 2, nw), codec.Nullable(codec.Signed[int64]()), nil)
	windowGolden(t, expr.Over(expr.Count[rowset.Alias[expressionSide, models.Order]]().Filter(expr.OrderRow(nv).Gt(0)), nw), codec.Signed[int64](), int64(2))
	windowGolden(t, expr.Over(expr.SumAt(ns, models.Orders.TotalCents), nw), codec.Nullable(codec.Decimal()), "12")
	windowGolden(t, expr.Over(expr.AvgAt(ns, models.Orders.TotalCents), nw), codec.Nullable(codec.Decimal()), "6")
	windowGolden(t, expr.Over(expr.MinAt(ns, models.Orders.TotalCents), nw), codec.Nullable(codec.Signed[int64]()), int64(5))
	windowGolden(t, expr.Over(expr.MaxAt(ns, models.Orders.TotalCents), nw), codec.Nullable(codec.Signed[int64]()), int64(7))
	windowGolden(t, expr.Over(expr.Exists[rowset.Alias[expressionSide, models.Order]](), nw), codec.Bool[bool](), true)
	windowGolden(t, expr.Over(expr.CountFieldAt(ns, models.Orders.TotalCents), nw), codec.Signed[int64](), int64(2))
	windowGolden(t, expr.RowNumber(nw.Named("framed")), codec.Signed[int64](), int64(1))

	nb := expr.WindowFor(ns).OrderBy(nv.Asc()).Named("base")
	windowGolden(t, expr.RowNumber(nb.GroupsBetween(expr.CurrentRow(), expr.Following(1)).ExcludeGroup()), codec.Signed[int64](), int64(1))
	windowGolden(t, expr.RowNumber(nb.RangeBetween(expr.UnboundedPreceding(), expr.UnboundedFollowing()).ExcludeCurrentRow()), codec.Signed[int64](), int64(1))

	nr := expr.NumericRange(expr.WindowFor(ns), expr.Row(nv).Value()).Desc()
	windowGolden(t, expr.RowNumber(nr.Between(nr.Preceding(5), nr.Following(10))), codec.Signed[int64](), int64(1))
	// Nullable helpers preserve one wrapper and the input codec.
	windowGolden(t, expr.LagNullable(expr.Nullable(expr.Row(nv).Value()), 1, nw), codec.Nullable(codec.Signed[int64]()), nil)
	windowGolden(t, expr.LeadOr(expr.Nullable(expr.Row(nv).Value()), 1, value.Of(int64(1)), nw), codec.Nullable(codec.Signed[int64]()), int64(1))
}
