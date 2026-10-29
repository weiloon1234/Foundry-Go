package goway_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"database/sql/driver"
	"fmt"
	"testing"

	"foundry.test/goway/models"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type aggregateGoldenResult struct {
	Buyer    string
	Count    int64
	Distinct int64
	Total    value.Nullable[decimal.Decimal]
	Average  value.Nullable[decimal.Decimal]
	Min, Max value.Nullable[int64]
}
type aggregateBuyerResult struct {
	Buyer           model.ID[models.User]
	Count, Distinct int64
	Total, Average  value.Nullable[decimal.Decimal]
	Min, Max        value.Nullable[int64]
}
type aggregateBuyerFields struct {
	rowset.Row[aggregateBuyerResult]
	Buyer           rowset.Field[aggregateBuyerResult, model.ID[models.User]]
	Count, Distinct rowset.Field[aggregateBuyerResult, int64]
	Total, Average  rowset.Field[aggregateBuyerResult, value.Nullable[decimal.Decimal]]
	Min, Max        rowset.Field[aggregateBuyerResult, value.Nullable[int64]]
}

var aggregateBuyerRows = rowset.Define(func(b rowset.Builder[aggregateBuyerResult], r *aggregateBuyerResult) aggregateBuyerFields {
	return aggregateBuyerFields{b.Row(), rowset.IDOf(b, &r.Buyer), rowset.NumberOf(b, &r.Count), rowset.NumberOf(b, &r.Distinct), rowset.NullableNumberOf(b, &r.Total), rowset.NullableNumberOf(b, &r.Average), rowset.NullableNumberOf(b, &r.Min), rowset.NullableNumberOf(b, &r.Max)}
})

func TestStage6a3AggregateGolden(t *testing.T) {
	after := &goldenCapture{}
	for _, c := range []*goldenCapture{after} {
		c.scriptedColumns = []string{"buyer", "count", "distinct", "total", "average", "min", "max"}
		c.scriptedBatches = [][][]driver.Value{{{buyerID, int64(2), int64(2), "1000", "500", int64(400), int64(600)}}}
	}

	runAggregateNew(after)
	if after.statements != 1 || !oracleEqual(t, "aggregate_golden/1", after.output.String()) {
		t.Fatal("frozen legacy comparison differs")
	}

	// Complete-model DISTINCT and DISTINCT ON keep their result read-only.
	after = &goldenCapture{}
	ctx := t.Context()
	newDB := after.open(ctx)

	defer newDB.Close(ctx)

	newBuyer, newAmount := expr.Value(orm.ColumnExpr(models.Orders.BuyerID)), expr.Value(orm.NumberExpr(models.Orders.TotalCents))
	newQ := rowset.OrderBy(rowset.DistinctOn(rowset.FromModel(models.Orders.Query()), expr.Group(newBuyer)), newBuyer.Asc(), newAmount.Desc())

	fmt.Fprintln(&after.output, oracleSnapshot([]any{must(rowset.Read(newQ).All(ctx, newDB))}))

	fmt.Fprintln(&after.output, oracleSnapshot([]any{must(rowset.Read(rowset.Distinct(rowset.FromModel(models.Orders.Query()))).All(ctx, newDB))}))
	if !oracleEqual(t, "aggregate_golden/2", after.output.String()) {
		t.Fatal("frozen legacy comparison differs")
	}
}

func runAggregateNew(g *goldenCapture) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	a := rowset.As[expressionSide](models.Orders.Query(), "o")
	scope := rowset.Scope(a)
	amount := expr.OrderRow(expr.At(scope, orm.NumberExpr(models.Orders.TotalCents)))
	buyer := expr.At(scope, orm.ColumnExpr(models.Orders.BuyerID))
	count := expr.Count[rowset.Alias[expressionSide, models.Order]]().Filter(amount.Gt(0))
	total := expr.SumAt(scope, orm.NumberExpr(models.Orders.TotalCents)).Filter(amount.Gt(100))
	s := aggregateBuyerRows
	q := rowset.Select(a, s, rowset.Map(s.Buyer, buyer), rowset.MapSelected(s.Count, count.Value()), rowset.MapSelected(s.Distinct, expr.CountDistinctFieldAt(scope, orm.NumberExpr(models.Orders.TotalCents)).Value()), rowset.MapSelected(s.Total, total.Value()), rowset.MapSelected(s.Average, expr.AvgAt(scope, orm.NumberExpr(models.Orders.TotalCents)).Value()), rowset.MapSelected(s.Min, expr.MinAt(scope, orm.NumberExpr(models.Orders.TotalCents)).Value()), rowset.MapSelected(s.Max, expr.MaxAt(scope, orm.NumberExpr(models.Orders.TotalCents)).Value()))
	q = rowset.OrderBy(rowset.Having(rowset.GroupBy(q, expr.Group(buyer)), expr.HavingAnd(count.Gt(0), expr.HavingOr(total.IsNull(), total.Gt(decimal.FromInt64(1))))), count.Desc(), buyer.Asc())
	got := must(rowset.Read(q).All(ctx, db))
	// Owners differ between APIs; compare the same concrete printable row shape.
	normalized := make([]aggregateGoldenResult, len(got))
	for i, r := range got {
		normalized[i] = aggregateGoldenResult{r.Buyer.String(), r.Count, r.Distinct, r.Total, r.Average, r.Min, r.Max}
	}
	fmt.Fprintln(&g.output, oracleSnapshot([]any{normalized}))
}
