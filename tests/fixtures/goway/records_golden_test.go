package goway_test

import (
	"context"
	"database/sql/driver"
	"fmt"
	"testing"

	"foundry.test/goway/models"
	old "foundry.test/goway/oldmodels"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestStage6c1RecordGolden(t *testing.T) {
	for _, kind := range []string{"model", "window", "projection", "transaction_model", "transaction_projection"} {
		t.Run(kind, func(t *testing.T) {
			before, after := &goldenCapture{}, &goldenCapture{}
			for _, g := range []*goldenCapture{before, after} {
				if kind == "projection" || kind == "transaction_projection" {
					g.scriptedColumns = []string{"amount"}
					g.scriptedBatches = [][][]driver.Value{{{int64(500)}}}
				} else {
					g.scriptedColumns = []string{"id", "buyer_id", "total_cents"}
					g.scriptedBatches = [][][]driver.Value{{{orderID, buyerID, int64(500)}}}
				}
			}
			runRecordOld(before, kind)
			runRecordNew(after, kind)
			if before.statements != 1 || after.statements != 1 || before.output.String() != after.output.String() {
				t.Fatalf("record parity:\n%s", goldenDiff(before.output.String(), after.output.String()))
			}
		})
	}
}
func runRecordOld(g *goldenCapture, kind string) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	q := old.QueryOrders()
	if kind == "window" {
		q = q.Where(old.OrderFields().TotalCents.Gt(10)).OrderBy(old.OrderFields().TotalCents.Desc()).Limit(3).Offset(1)
	}
	if kind == "projection" {
		projected := oldCorrelationProjection(q, query.Nullable(query.Value(old.OrderFields().TotalCents))).OrderBy(old.OrderFields().TotalCents.Desc()).Limit(3).Offset(1)
		a := query.As[joinA](projected, "r")
		fmt.Fprintln(&g.output, must(query.SelectRecord(a, a.Scope()).Limit(2).All(ctx, db)))
		return
	}
	if kind == "transaction_model" || kind == "transaction_projection" {
		check(db.Transaction(ctx, func(tx *database.Tx) error {
			a := query.AsTransaction[joinA](q.Where(old.OrderFields().TotalCents.Gt(10)).ForUpdate().SkipLocked(), "r")
			if kind == "transaction_projection" {
				field := query.NewProjectionField[correlationGoldenRow, value.Nullable[int64]]("amount")
				def := query.DefineProjection([]query.ProjectionColumn[correlationGoldenRow]{field.Column()}, func(row database.Row) (correlationGoldenRow, error) {
					var r correlationGoldenRow
					err := row.Scan(codec.Nullable(codec.Signed[int64]()).Scan(&r.Amount))
					return r, err
				})
				selected := query.ProjectTransaction(a, def, query.Map(field, query.Nullable(query.Value(old.OrderFieldsAt(a.Scope()).TotalCents))))
				fmt.Fprintln(&g.output, must(selected.Limit(2).All(ctx, tx)))
			} else {
				got := must(query.SelectTransactionRecord(a, a.Scope()).Limit(2).All(ctx, tx))
				for _, v := range got {
					fmt.Fprintln(&g.output, v.ID, v.BuyerID, v.TotalCents)
				}
			}
			return nil
		}))
		return
	}
	a := query.As[joinA](q, "r")
	got := must(query.SelectRecord(a, a.Scope()).All(ctx, db))
	for _, v := range got {
		fmt.Fprintln(&g.output, v.ID, v.BuyerID, v.TotalCents)
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
		projected := rowset.Offset(rowset.Limit(rowset.Select(rowset.From(q), correlationGoldenRows, rowset.MapSelected(correlationGoldenRows.Amount, expr.Nullable(expr.Select(expr.Value(models.Orders.TotalCents))))).OrderBy(expr.Value(models.Orders.TotalCents).Desc()), 3), 1)
		a := rowset.AsRecord[joinA](rowset.RecordOf(projected), "r")
		fmt.Fprintln(&g.output, must(rowset.SelectRecord(a, rowset.Scope(a)).Limit(2).All(ctx, db)))
		return
	}
	if kind == "transaction_model" || kind == "transaction_projection" {
		check(db.Transaction(ctx, func(tx *database.Tx) error {
			a := rowset.AsTransaction[joinA](rowset.LockedRecord(q.Where(models.Orders.TotalCents.Gt(10)).ForUpdate().SkipLocked()), "r")
			if kind == "transaction_projection" {
				selected := rowset.SelectTransaction(a, correlationGoldenRows, rowset.MapSelected(correlationGoldenRows.Amount, expr.Nullable(expr.Select(expr.At(rowset.TransactionScope(a), models.Orders.TotalCents)))))
				fmt.Fprintln(&g.output, must(selected.Limit(2).All(ctx, tx)))
			} else {
				got := must(rowset.SelectTransactionRecord(a, rowset.TransactionScope(a)).Limit(2).All(ctx, tx))
				for _, v := range got {
					fmt.Fprintln(&g.output, v.ID, v.BuyerID, v.TotalCents)
				}
			}
			return nil
		}))
		return
	}
	a := rowset.AsRecord[joinA](rowset.ModelRecord(q), "r")
	got := must(rowset.SelectRecord(a, rowset.Scope(a)).All(ctx, db))
	for _, v := range got {
		fmt.Fprintln(&g.output, v.ID, v.BuyerID, v.TotalCents)
	}
}
