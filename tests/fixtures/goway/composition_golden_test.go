package goway_test

import (
	"context"
	"database/sql/driver"
	"fmt"
	"foundry.test/goway/models"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"strings"
	"testing"
)

func TestStage6c2CompositionGolden(t *testing.T) {
	for _, kind := range []string{"set_Union", "set_UnionAll", "set_Intersect", "set_IntersectAll", "set_Except", "set_ExceptAll", "value_Union", "value_UnionAll", "value_Intersect", "value_IntersectAll", "value_Except", "value_ExceptAll", "cte", "materialized", "dependency", "recursive", "recursive_all", "transaction", "projection"} {
		t.Run(kind, func(t *testing.T) {
			after := &goldenCapture{}
			for _, g := range []*goldenCapture{after} {
				switch {
				case strings.HasPrefix(kind, "value_"):
					g.scriptedColumns = []string{"value"}
					g.scriptedBatches = [][][]driver.Value{{{int64(500)}}}
				case kind == "projection":
					g.scriptedColumns = []string{"amount"}
					g.scriptedBatches = [][][]driver.Value{{{int64(500)}}}
				default:
					g.scriptedColumns = []string{"id", "buyer_id", "total_cents"}
					g.scriptedBatches = [][][]driver.Value{{{orderID, buyerID, int64(500)}}}
				}
			}

			runCompositionNew(after, kind)
			if after.statements != 1 || !oracleEqual(t, "composition_golden/1", after.output.String()) {
				t.Fatal("frozen legacy comparison differs")
			}
		})
	}
}

func runCompositionNew(g *goldenCapture, kind string) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	a := models.Orders.Query().Where(models.Orders.TotalCents.Gt(10)).OrderBy(models.Orders.TotalCents.Desc()).Limit(2).Offset(1)
	b := models.Orders.Query().Where(models.Orders.TotalCents.Gt(20))
	if strings.HasPrefix(kind, "value_") {
		x, y := rowset.Values(rowset.SelectScalar(rowset.From(a), expr.Row(expr.Value(models.Orders.TotalCents)).Value())), rowset.Values(rowset.SelectScalar(rowset.From(b), expr.Row(expr.Value(models.Orders.TotalCents)).Value()))
		var q rowset.ValueSetQuery[int64]
		switch kind {
		case "value_Union":
			q = rowset.UnionValues(x, y)
		case "value_UnionAll":
			q = rowset.UnionAllValues(x, y)
		case "value_Intersect":
			q = rowset.IntersectValues(x, y)
		case "value_IntersectAll":
			q = rowset.IntersectAllValues(x, y)
		case "value_Except":
			q = rowset.ExceptValues(x, y)
		case "value_ExceptAll":
			q = rowset.ExceptAllValues(x, y)
		}
		fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.ReadValues(q.OrderBy(q.Value().Asc()).Limit(3).Offset(2)).All(ctx, db))}))
		return
	}
	if strings.HasPrefix(kind, "set_") {
		x, y := rowset.ModelRecord(a), rowset.ModelRecord(b)
		var q rowset.SetQuery[models.Order]
		switch kind {
		case "set_Union":
			q = rowset.Union(x, y)
		case "set_UnionAll":
			q = rowset.UnionAll(x, y)
		case "set_Intersect":
			q = rowset.Intersect(x, y)
		case "set_IntersectAll":
			q = rowset.IntersectAll(x, y)
		case "set_Except":
			q = rowset.Except(x, y)
		case "set_ExceptAll":
			q = rowset.ExceptAll(x, y)
		}
		got := must(rowset.ReadSet(q.Where(expr.At(q.Scope(), models.Orders.TotalCents).Ne(0)).OrderBy(expr.At(q.Scope(), models.Orders.TotalCents).Asc()).Limit(3).Offset(2)).All(ctx, db))
		for _, r := range got {
			fmt.Fprintln(&g.output, oracleSnapshot([]any{r.ID, r.BuyerID, r.TotalCents}))
		}
		return
	}
	if kind == "transaction" {
		check(db.Transaction(ctx, func(tx *database.Tx) error {
			c := rowset.TransactionCTE("claimed", rowset.LockedRecord(a.ForUpdate().SkipLocked())).Materialized()
			alias := rowset.AsTransaction[joinA](c, "r")
			for _, r := range must(rowset.SelectTransactionRecord(alias, rowset.TransactionScope(alias)).All(ctx, tx)) {
				fmt.Fprintln(&g.output, oracleSnapshot([]any{r.ID, r.BuyerID, r.TotalCents}))
			}
			return nil
		}))
		return
	}
	if kind == "projection" {
		p := rowset.Select(rowset.From(a), correlationGoldenRows, rowset.MapSelected(correlationGoldenRows.Amount, expr.Nullable(expr.Row(expr.Value(models.Orders.TotalCents)).Value())))
		alias := rowset.AsRecord[joinA](rowset.CTE("summary", rowset.RecordOf(p)), "r")
		fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.ReadRecord(rowset.SelectRecord(alias, rowset.Scope(alias))).All(ctx, db))}))
		return
	}
	c := rowset.CTE("eligible", rowset.ModelRecord(a))
	switch kind {
	case "materialized":
		c = c.Materialized()
	case "dependency":
		c = rowset.CTE("dependent", c.NotMaterialized())
	case "recursive", "recursive_all":
		step := func(self rowset.RecursiveSelf[models.Order]) rowset.RecordSource[models.Order] {
			p := rowset.AsRecord[joinB](self, "parent")
			return rowset.SelectRecord(p, rowset.Scope(p)).Where(expr.At(rowset.Scope(p), models.Orders.TotalCents).Eq(0))
		}
		if kind == "recursive" {
			c = rowset.RecursiveCTE("eligible", rowset.ModelRecord(a), step)
		} else {
			c = rowset.RecursiveAllCTE("eligible", rowset.ModelRecord(a), step)
		}
	}
	alias := rowset.AsRecord[joinA](c, "r")
	for _, r := range must(rowset.ReadRecord(rowset.SelectRecord(alias, rowset.Scope(alias))).All(ctx, db)) {
		fmt.Fprintln(&g.output, oracleSnapshot([]any{r.ID, r.BuyerID, r.TotalCents}))
	}
}
