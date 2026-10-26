package goway_test

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
	"testing"

	"foundry.test/goway/models"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestStage6c3ResultGolden(t *testing.T) {
	for _, kind := range []string{"cursor_record", "cursor_set", "cursor_cte", "cursor_value", "page", "simple_page", "first", "require_first", "count", "exists", "lock", "transaction_value", "transaction_value_lock"} {
		t.Run(kind, func(t *testing.T) {
			after := &goldenCapture{}
			for _, g := range []*goldenCapture{after} {
				g.scriptedColumns = []string{"value"}
				g.scriptedBatches = [][][]driver.Value{{{int64(500)}, {int64(600)}}}
				if strings.HasPrefix(kind, "cursor_") && kind != "cursor_value" {
					g.scriptedColumns = []string{"id", "buyer_id", "total_cents"}
					g.scriptedBatches = [][][]driver.Value{{{orderID, buyerID, int64(500)}, {buyerID, buyerID, int64(600)}}, {{buyerID, buyerID, int64(600)}}, {{orderID, buyerID, int64(500)}}}
				}
				if kind == "page" {
					g.scriptedBatches = [][][]driver.Value{{{int64(2)}}, {{int64(500)}}}
				}
				if kind == "exists" {
					g.scriptedBatches = [][][]driver.Value{{{true}}}
				}
				if kind == "count" || kind == "first" || kind == "require_first" || kind == "lock" || strings.HasPrefix(kind, "transaction_") {
					g.scriptedBatches = [][][]driver.Value{{{int64(500)}}}
				}
			}

			runResultNew(after, kind)
			if !oracleEqual(t, "result_read_golden/1", after.output.String()) {
				t.Fatal("frozen legacy comparison differs")
			}
		})
	}
}

func runResultNew(g *goldenCapture, kind string) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	base := models.Orders.Query().Where(models.Orders.TotalCents.Gt(10))
	if strings.HasPrefix(kind, "cursor_") && kind != "cursor_value" {
		var source rowset.RecordSource[models.Order] = rowset.ModelRecord(base.Limit(9).Offset(1))
		if kind == "cursor_set" {
			source = rowset.Union(source, rowset.ModelRecord(base))
		}
		if kind == "cursor_cte" {
			source = rowset.CTE("report", source)
		}
		c := rowset.CursorFor(source)
		id := expr.At(c.Scope(), models.Orders.ID)
		amount := expr.At(c.Scope(), models.Orders.TotalCents)
		c = c.OrderBy(amount.Asc()).UniqueBy(expr.Group(id))
		p := must(c.Paginate(ctx, db, orm.CursorRequest[models.Order]{Size: 1}))
		printNewCursor(g, p)
		p = must(c.Paginate(ctx, db, orm.CursorRequest[models.Order]{Size: 1, After: p.Next}))
		printNewCursor(g, p)
		p = must(c.Paginate(ctx, db, orm.CursorRequest[models.Order]{Size: 1, Before: p.Previous}))
		printNewCursor(g, p)
		return
	}
	selected := rowset.SelectScalar(rowset.From(base.OrderBy(models.Orders.TotalCents.Asc())), expr.Select(expr.Value(models.Orders.TotalCents)))
	q := rowset.ReadScalar(selected)
	switch kind {
	case "cursor_value":
		c := rowset.ValueCursorFor(rowset.Values(selected))
		p := must(c.UniqueBy(c.Key()).Paginate(ctx, db, orm.CursorRequest[int64]{Size: 1}))
		fmt.Fprintln(&g.output, oracleSnapshot([]any{p.Items, p.Next.IsSet(), p.Previous.IsSet()}))
	case "page":
		fmt.Fprintln(&g.output, oracleSnapshot([]any{must(q.Paginate(ctx, db, orm.PageRequest{Number: 1, Size: 1}))}))
	case "simple_page":
		fmt.Fprintln(&g.output, oracleSnapshot([]any{must(q.SimplePaginate(ctx, db, orm.PageRequest{Number: 1, Size: 1}))}))
	case "first":
		fmt.Fprintln(&g.output, oracleSnapshot([]any{must(q.First(ctx, db))}))
	case "require_first":
		fmt.Fprintln(&g.output, oracleSnapshot([]any{must(q.RequireFirst(ctx, db))}))
	case "count":
		fmt.Fprintln(&g.output, oracleSnapshot([]any{must(q.Limit(2).Count(ctx, db))}))
	case "exists":
		fmt.Fprintln(&g.output, oracleSnapshot([]any{must(q.Exists(ctx, db))}))
	case "lock":
		check(db.Transaction(ctx, func(tx *database.Tx) error {
			fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.LockRows(q, rowset.UpdateLock(rowset.Target(rowset.ModelScope(base))).SkipLocked()).All(ctx, tx))}))
			return nil
		}))
	default:
		check(db.Transaction(ctx, func(tx *database.Tx) error {
			a := rowset.AsTransaction[joinA](rowset.TransactionOf(rowset.ModelRecord(base)), "selected")
			v := rowset.SelectTransactionValue(a, expr.Select(expr.At(rowset.TransactionScope(a), models.Orders.TotalCents)))
			if kind == "transaction_value_lock" {
				fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.ForShare(v).All(ctx, tx))}))
			} else {
				fmt.Fprintln(&g.output, oracleSnapshot([]any{must(v.All(ctx, tx))}))
			}
			return nil
		}))
	}
}
func printNewCursor(g *goldenCapture, p orm.CursorPage[models.Order]) {
	for _, r := range p.Items {
		fmt.Fprintln(&g.output, oracleSnapshot([]any{r.ID, r.BuyerID, r.TotalCents}))
	}
	fmt.Fprintln(&g.output, oracleSnapshot([]any{p.Next.IsSet(), p.Previous.IsSet()}))
}
