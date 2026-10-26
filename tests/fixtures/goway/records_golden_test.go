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

func TestStage6c1RecordGolden(t *testing.T) {
	for _, kind := range []string{"model", "window", "projection", "transaction_model", "transaction_projection"} {
		t.Run(kind, func(t *testing.T) {
			after := &goldenCapture{}
			for _, g := range []*goldenCapture{after} {
				if kind == "projection" || kind == "transaction_projection" {
					g.scriptedColumns = []string{"amount"}
					g.scriptedBatches = [][][]driver.Value{{{int64(500)}}}
				} else {
					g.scriptedColumns = []string{"id", "buyer_id", "total_cents"}
					g.scriptedBatches = [][][]driver.Value{{{orderID, buyerID, int64(500)}}}
				}
			}

			runRecordNew(after, kind)
			if after.statements != 1 || !oracleEqual(t, "records_golden/1", after.output.String()) {
				t.Fatal("frozen legacy comparison differs")
			}
		})
	}
}

func runRecordNew(g *goldenCapture, kind string) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	q := models.Orders.Query()
	if kind == "window" {
		q = q.Where(models.Orders.TotalCents.Gt(10)).OrderBy(models.Orders.TotalCents.Desc()).Limit(3).Offset(1)
	}
	if kind == "projection" {
		projected := rowset.Offset(rowset.Limit(rowset.Select(rowset.From(q), correlationGoldenRows, rowset.MapSelected(correlationGoldenRows.Amount, expr.Nullable(expr.Row(expr.Value(models.Orders.TotalCents)).Value()))).OrderBy(expr.Value(models.Orders.TotalCents).Desc()), 3), 1)
		a := rowset.AsRecord[joinA](rowset.RecordOf(projected), "r")
		fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.ReadRecord(rowset.SelectRecord(a, rowset.Scope(a)).Limit(2)).All(ctx, db))}))
		return
	}
	if kind == "transaction_model" || kind == "transaction_projection" {
		check(db.Transaction(ctx, func(tx *database.Tx) error {
			a := rowset.AsTransaction[joinA](rowset.LockedRecord(q.Where(models.Orders.TotalCents.Gt(10)).ForUpdate().SkipLocked()), "r")
			if kind == "transaction_projection" {
				selected := rowset.SelectTransaction(a, correlationGoldenRows, rowset.MapSelected(correlationGoldenRows.Amount, expr.Nullable(expr.Row(expr.At(rowset.TransactionScope(a), models.Orders.TotalCents)).Value())))
				fmt.Fprintln(&g.output, oracleSnapshot([]any{must(selected.Limit(2).All(ctx, tx))}))
			} else {
				got := must(rowset.SelectTransactionRecord(a, rowset.TransactionScope(a)).Limit(2).All(ctx, tx))
				for _, v := range got {
					fmt.Fprintln(&g.output, oracleSnapshot([]any{v.ID, v.BuyerID, v.TotalCents}))
				}
			}
			return nil
		}))
		return
	}
	a := rowset.AsRecord[joinA](rowset.ModelRecord(q), "r")
	got := must(rowset.ReadRecord(rowset.SelectRecord(a, rowset.Scope(a))).All(ctx, db))
	for _, v := range got {
		fmt.Fprintln(&g.output, oracleSnapshot([]any{v.ID, v.BuyerID, v.TotalCents}))
	}
}
