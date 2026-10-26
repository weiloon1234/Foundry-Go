package goway_test

import (
	"context"
	"database/sql/driver"
	"fmt"
	"testing"

	"foundry.test/goway/models"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestStage6b4TransactionCompositionGolden(t *testing.T) {
	for _, kind := range []string{"lift", "inner", "left", "right", "full", "cross", "scalar", "nullable_scalar", "membership", "exists", "correlated", "nested", "lateral_cross", "lateral_inner", "lateral_left", "lateral_on"} {
		t.Run(kind, func(t *testing.T) {
			b := &goldenCapture{}
			for _, g := range []*goldenCapture{b} {
				g.scriptedColumns = []string{"value"}
				g.scriptedBatches = [][][]driver.Value{{{int64(9)}, {nil}}}
			}
			runTransactionCompositionNew(b, kind)
			if b.statements != 1 || !oracleEqual(t, "transaction_composition_golden/1", b.output.String()) {
				t.Fatal("frozen legacy comparison differs")
			}
		})
	}
}

func runTransactionCompositionNew(g *goldenCapture, kind string) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	check(db.Transaction(ctx, func(tx *database.Tx) error {
		a := rowset.AsTransaction[joinA](rowset.LockedRecord(models.Orders.Query().Where(models.Orders.TotalCents.Gt(5)).Limit(2).ForUpdate().SkipLocked()), "a")
		b := rowset.AsTransaction[joinB](rowset.TransactionOf(rowset.ModelRecord(models.Orders.Query().Limit(3))), "b")
		as, bs := rowset.TransactionScope(a), rowset.TransactionScope(b)
		x, y := expr.At(as, models.Orders.TotalCents), expr.At(bs, models.Orders.TotalCents)
		on := rowset.On(expr.At(as, models.Orders.BuyerID), expr.At(bs, models.Orders.BuyerID))
		switch kind {
		case "lift":
			lifted := rowset.TransactionValueOf(rowset.Values(rowset.SelectScalar(rowset.From(models.Orders.Query().Limit(1)), expr.Row(expr.Value(models.Orders.TotalCents)).Value())))
			fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.SelectTransactionValue(a, rowset.TransactionScalarQuery(a, lifted)).All(ctx, tx))}))
		case "inner":
			j := rowset.TransactionInnerJoin(a, b, on)
			fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.SelectTransactionValue(j, expr.Nullable(expr.Row(expr.At(rowset.LeftScope(j, as), models.Orders.TotalCents)).Value())).All(ctx, tx))}))
		case "left":
			j := rowset.TransactionLeftJoin(a, b, on)
			fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.SelectTransactionValue(j, expr.Row(expr.AtNullable(rowset.NullableRightScope(j, bs), models.Orders.TotalCents)).Value()).All(ctx, tx))}))
		case "right":
			j := rowset.TransactionRightJoin(a, b, on)
			fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.SelectTransactionValue(j, expr.Nullable(expr.Row(expr.At(rowset.RightScope(j, bs), models.Orders.TotalCents)).Value())).All(ctx, tx))}))
		case "full":
			j := rowset.TransactionFullJoin(a, b, on)
			fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.SelectTransactionValue(j, expr.Row(expr.AtNullable(rowset.NullableRightScope(j, bs), models.Orders.TotalCents)).Value()).All(ctx, tx))}))
		case "cross":
			j := rowset.TransactionCrossJoin(a, b)
			fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.SelectTransactionValue(j, expr.Nullable(expr.Row(expr.At(rowset.RightScope(j, bs), models.Orders.TotalCents)).Value())).All(ctx, tx))}))
		case "scalar", "nullable_scalar", "membership", "exists":
			v := rowset.SelectTransactionValue(b, expr.Row(y).Value()).Where(expr.Greater(y, expr.Row(y).Value().Param(1))).Limit(1)
			locked := rowset.TransactionScalarOf(v).ForShare().NoWait()
			q := rowset.SelectTransactionValue(a, rowset.TransactionScalarQuery(a, locked))
			if kind == "nullable_scalar" {
				n := rowset.SelectTransactionValue(b, expr.Nullable(expr.Row(y).Value())).Where(expr.Greater(y, expr.Row(y).Value().Param(1))).Limit(1)
				q = rowset.SelectTransactionValue(a, rowset.TransactionScalarNullableQuery(a, rowset.TransactionScalarOf(n).ForShare().NoWait()))
			}
			if kind == "membership" {
				q = q.Where(rowset.TransactionInQuery(x, locked))
			}
			if kind == "exists" {
				q = q.Where(rowset.TransactionExistsQuery(a, locked))
			}
			fmt.Fprintln(&g.output, oracleSnapshot([]any{must(q.All(ctx, tx))}))
		default:
			c := rowset.TransactionCorrelate(a, b)
			is := rowset.TransactionInnerScope(c, bs)
			inner := expr.At(is, models.Orders.TotalCents)
			outer := expr.At(rowset.TransactionOuterScope(c, as), models.Orders.TotalCents)
			c = c.Where(expr.Less(inner, outer))
			if kind == "nested" {
				d := rowset.AsTransaction[joinC](rowset.TransactionOf(rowset.ModelRecord(models.Orders.Query())), "d")
				n := rowset.TransactionCorrelate(c, d)
				n = n.Where(expr.Equal(expr.At(rowset.TransactionInnerScope(n, rowset.TransactionScope(d)), models.Orders.TotalCents), expr.At(rowset.TransactionOuterScope(n, is), models.Orders.TotalCents)))
				c = c.Where(n.Exists())
			}
			if kind == "correlated" || kind == "nested" {
				v := rowset.SelectTransactionCorrelatedScalar(c, expr.Row(inner).Value()).Limit(1).ForShare().NoWait()
				fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.SelectTransactionValue(a, rowset.TransactionCorrelatedScalarQuery(v)).Where(v.Exists(), rowset.TransactionInCorrelatedQuery(x, v)).All(ctx, tx))}))
				return nil
			}
			r := rowset.SelectTransactionCorrelatedRecord(c, is).OrderBy(inner.Desc()).Limit(1).ForUpdate().Of(rowset.Target(is)).SkipLocked()
			lat := rowset.AsTransactionLateral[joinC](r, "latest")
			ls := rowset.TransactionLateralScope(lat)
			switch kind {
			case "lateral_cross":
				j := rowset.TransactionCrossJoinLateral(a, lat)
				fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.SelectTransactionValue(j, expr.Nullable(expr.Row(expr.At(rowset.RightScope(j, ls), models.Orders.TotalCents)).Value())).All(ctx, tx))}))
			case "lateral_inner":
				j := rowset.TransactionInnerJoinLateral(a, lat)
				fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.SelectTransactionValue(j, expr.Nullable(expr.Row(expr.At(rowset.RightScope(j, ls), models.Orders.TotalCents)).Value())).All(ctx, tx))}))
			case "lateral_left":
				j := rowset.TransactionLeftJoinLateral(a, lat)
				fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.SelectTransactionValue(j, expr.Row(expr.AtNullable(rowset.NullableRightScope(j, ls), models.Orders.TotalCents)).Value()).All(ctx, tx))}))
			case "lateral_on":
				j := rowset.TransactionLeftJoinLateral(a, lat, rowset.On(x, expr.At(ls, models.Orders.TotalCents)))
				fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.SelectTransactionValue(j, expr.Row(expr.AtNullable(rowset.NullableRightScope(j, ls), models.Orders.TotalCents)).Value()).All(ctx, tx))}))
			}
		}
		return nil
	}))
}
