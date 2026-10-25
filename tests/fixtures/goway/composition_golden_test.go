package goway_test

import (
	"context"
	"database/sql/driver"
	"fmt"
	"foundry.test/goway/models"
	old "foundry.test/goway/oldmodels"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"strings"
	"testing"
)

func TestStage6c2CompositionGolden(t *testing.T) {
	for _, kind := range []string{"set_Union", "set_UnionAll", "set_Intersect", "set_IntersectAll", "set_Except", "set_ExceptAll", "value_Union", "value_UnionAll", "value_Intersect", "value_IntersectAll", "value_Except", "value_ExceptAll", "cte", "materialized", "dependency", "recursive", "recursive_all", "transaction", "projection"} {
		t.Run(kind, func(t *testing.T) {
			before, after := &goldenCapture{}, &goldenCapture{}
			for _, g := range []*goldenCapture{before, after} {
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
			runCompositionOld(before, kind)
			runCompositionNew(after, kind)
			if before.statements != 1 || after.statements != 1 || !oracleEqual(t, "composition_golden/1", before.output.String(), after.output.String()) {
				t.Fatalf("composition parity:\n%s", goldenDiff(before.output.String(), after.output.String()))
			}
		})
	}
}
func runCompositionOld(g *goldenCapture, kind string) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	a := old.QueryOrders().Where(old.OrderFields().TotalCents.Gt(10)).OrderBy(old.OrderFields().TotalCents.Desc()).Limit(2).Offset(1)
	b := old.QueryOrders().Where(old.OrderFields().TotalCents.Gt(20))
	if strings.HasPrefix(kind, "value_") {
		x, y := query.SelectValue(a, query.Value(old.OrderFields().TotalCents)), query.SelectValue(b, query.Value(old.OrderFields().TotalCents))
		var q query.ValueSetQuery[int64]
		switch kind {
		case "value_Union":
			q = x.Union(y)
		case "value_UnionAll":
			q = x.UnionAll(y)
		case "value_Intersect":
			q = x.Intersect(y)
		case "value_IntersectAll":
			q = x.IntersectAll(y)
		case "value_Except":
			q = x.Except(y)
		case "value_ExceptAll":
			q = x.ExceptAll(y)
		}
		fmt.Fprintln(&g.output, oracleSnapshot([]any{must(q.OrderBy(q.Value().Asc()).Limit(3).Offset(2).All(ctx, db))}))
		return
	}
	if strings.HasPrefix(kind, "set_") {
		var q query.SetQuery[old.Order]
		switch kind {
		case "set_Union":
			q = query.Union(a, b)
		case "set_UnionAll":
			q = query.UnionAll(a, b)
		case "set_Intersect":
			q = query.Intersect(a, b)
		case "set_IntersectAll":
			q = query.IntersectAll(a, b)
		case "set_Except":
			q = query.Except(a, b)
		case "set_ExceptAll":
			q = query.ExceptAll(a, b)
		}
		got := must(q.Where(old.OrderFieldsAt(q.Scope()).TotalCents.Ne(0)).OrderBy(old.OrderFieldsAt(q.Scope()).TotalCents.Asc()).Limit(3).Offset(2).All(ctx, db))
		for _, r := range got {
			fmt.Fprintln(&g.output, oracleSnapshot([]any{r.ID, r.BuyerID, r.TotalCents}))
		}
		return
	}
	if kind == "transaction" {
		check(db.Transaction(ctx, func(tx *database.Tx) error {
			c := query.TransactionCTE("claimed", a.ForUpdate().SkipLocked()).Materialized()
			alias := query.AsTransaction[joinA](c, "r")
			for _, r := range must(query.SelectTransactionRecord(alias, alias.Scope()).All(ctx, tx)) {
				fmt.Fprintln(&g.output, oracleSnapshot([]any{r.ID, r.BuyerID, r.TotalCents}))
			}
			return nil
		}))
		return
	}
	if kind == "projection" {
		p := oldCorrelationProjection(a, query.Nullable(query.Value(old.OrderFields().TotalCents)))
		alias := query.As[joinA](query.CTE("summary", p), "r")
		fmt.Fprintln(&g.output, oracleSnapshot([]any{must(query.SelectRecord(alias, alias.Scope()).All(ctx, db))}))
		return
	}
	c := query.CTE("eligible", a)
	switch kind {
	case "materialized":
		c = c.Materialized()
	case "dependency":
		c = query.CTE("dependent", c.NotMaterialized())
	case "recursive", "recursive_all":
		step := func(self query.RecursiveSelf[old.Order]) query.RecordQuerySource[old.Order] {
			p := query.As[joinB](self, "parent")
			return query.SelectRecord(p, p.Scope()).Where(old.OrderFieldsAt(p.Scope()).TotalCents.Eq(0))
		}
		if kind == "recursive" {
			c = query.RecursiveCTE("eligible", a, step)
		} else {
			c = query.RecursiveAllCTE("eligible", a, step)
		}
	}
	alias := query.As[joinA](c, "r")
	for _, r := range must(query.SelectRecord(alias, alias.Scope()).All(ctx, db)) {
		fmt.Fprintln(&g.output, oracleSnapshot([]any{r.ID, r.BuyerID, r.TotalCents}))
	}
}
func runCompositionNew(g *goldenCapture, kind string) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	a := models.Orders.Query().Where(models.Orders.TotalCents.Gt(10)).OrderBy(models.Orders.TotalCents.Desc()).Limit(2).Offset(1)
	b := models.Orders.Query().Where(models.Orders.TotalCents.Gt(20))
	if strings.HasPrefix(kind, "value_") {
		x, y := rowset.Values(rowset.SelectScalar(rowset.From(a), expr.Select(expr.Value(models.Orders.TotalCents)))), rowset.Values(rowset.SelectScalar(rowset.From(b), expr.Select(expr.Value(models.Orders.TotalCents))))
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
		fmt.Fprintln(&g.output, oracleSnapshot([]any{must(q.OrderBy(q.Value().Asc()).Limit(3).Offset(2).All(ctx, db))}))
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
		got := must(q.Where(expr.At(q.Scope(), models.Orders.TotalCents).Ne(0)).OrderBy(expr.At(q.Scope(), models.Orders.TotalCents).Asc()).Limit(3).Offset(2).All(ctx, db))
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
		p := rowset.Select(rowset.From(a), correlationGoldenRows, rowset.MapSelected(correlationGoldenRows.Amount, expr.Nullable(expr.Select(expr.Value(models.Orders.TotalCents)))))
		alias := rowset.AsRecord[joinA](rowset.CTE("summary", rowset.RecordOf(p)), "r")
		fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.SelectRecord(alias, rowset.Scope(alias)).All(ctx, db))}))
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
	for _, r := range must(rowset.SelectRecord(alias, rowset.Scope(alias)).All(ctx, db)) {
		fmt.Fprintln(&g.output, oracleSnapshot([]any{r.ID, r.BuyerID, r.TotalCents}))
	}
}
