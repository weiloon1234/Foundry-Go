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
	before, after := &goldenCapture{}, &goldenCapture{}
	for _, c := range []*goldenCapture{before, after} {
		c.scriptedColumns = []string{"buyer", "count", "distinct", "total", "average", "min", "max"}
		c.scriptedBatches = [][][]driver.Value{{{buyerID, int64(2), int64(2), "1000", "500", int64(400), int64(600)}}}
	}
	runAggregateOld(before)
	runAggregateNew(after)
	if before.statements != 1 || after.statements != 1 || before.output.String() != after.output.String() {
		t.Fatalf("aggregate parity:\n%s", goldenDiff(before.output.String(), after.output.String()))
	}
	// Complete-model DISTINCT and DISTINCT ON keep their result read-only.
	before, after = &goldenCapture{}, &goldenCapture{}
	ctx := t.Context()
	oldDB, newDB := before.open(ctx), after.open(ctx)
	defer oldDB.Close(ctx)
	defer newDB.Close(ctx)
	oldF := old.OrderFields()
	oldQ := old.QueryOrders().DistinctOn(oldF.BuyerID.Group()).OrderBy(oldF.BuyerID.Asc(), oldF.TotalCents.Desc())
	newBuyer, newAmount := expr.Value(models.Orders.BuyerID), expr.Value(models.Orders.TotalCents)
	newQ := rowset.DistinctOn(rowset.FromModel(models.Orders.Query()), expr.Group(newBuyer)).OrderBy(newBuyer.Asc(), newAmount.Desc())
	fmt.Fprintln(&before.output, must(oldQ.All(ctx, oldDB)))
	fmt.Fprintln(&after.output, must(newQ.All(ctx, newDB)))
	fmt.Fprintln(&before.output, must(old.QueryOrders().Distinct().All(ctx, oldDB)))
	fmt.Fprintln(&after.output, must(rowset.Distinct(rowset.FromModel(models.Orders.Query())).All(ctx, newDB)))
	if before.output.String() != after.output.String() {
		t.Fatalf("distinct parity:\n%s", goldenDiff(before.output.String(), after.output.String()))
	}
}
func runAggregateOld(g *goldenCapture) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	a := query.As[expressionSide](old.QueryOrders(), "o")
	f := old.OrderFieldsAt(a.Scope())
	count := query.Count[query.Alias[expressionSide, old.Order]]().Filter(f.TotalCents.Gt(0))
	total := query.Sum(f.TotalCents).Filter(f.TotalCents.Gt(100))
	buyer := query.NewProjectionField[aggregateGoldenResult, model.ID[old.User]]("buyer")
	countField := query.NewProjectionField[aggregateGoldenResult, int64]("count")
	distinct := query.NewProjectionField[aggregateGoldenResult, int64]("distinct")
	sum := query.NewProjectionField[aggregateGoldenResult, value.Nullable[decimal.Decimal]]("total")
	average := query.NewProjectionField[aggregateGoldenResult, value.Nullable[decimal.Decimal]]("average")
	min := query.NewProjectionField[aggregateGoldenResult, value.Nullable[int64]]("min")
	max := query.NewProjectionField[aggregateGoldenResult, value.Nullable[int64]]("max")
	schema := query.DefineProjection([]query.ProjectionColumn[aggregateGoldenResult]{buyer.Column(), countField.Column(), distinct.Column(), sum.Column(), average.Column(), min.Column(), max.Column()}, func(row database.Row) (aggregateGoldenResult, error) {
		var r aggregateGoldenResult
		var id model.ID[old.User]
		err := row.Scan(codec.ID[old.User]().Scan(&id), codec.Signed[int64]().Scan(&r.Count), codec.Signed[int64]().Scan(&r.Distinct), codec.Nullable(codec.Decimal()).Scan(&r.Total), codec.Nullable(codec.Decimal()).Scan(&r.Average), codec.Nullable(codec.Signed[int64]()).Scan(&r.Min), codec.Nullable(codec.Signed[int64]()).Scan(&r.Max))
		r.Buyer = id.String()
		return r, err
	})
	q := query.Project(a, schema, query.Map(buyer, query.Value(f.BuyerID)), query.Map(countField, count.Value()), query.Map(distinct, query.CountDistinctField(f.TotalCents).Value()), query.Map(sum, total.Value()), query.Map(average, query.Avg(f.TotalCents).Value()), query.Map(min, query.Min(f.TotalCents).Value()), query.Map(max, query.Max(f.TotalCents).Value())).GroupBy(f.BuyerID.Group()).Having(query.HavingAnd(count.Gt(0), query.HavingOr(total.IsNull(), total.Gt(decimal.FromInt64(1))))).OrderBy(count.Desc(), f.BuyerID.Asc())
	fmt.Fprintln(&g.output, must(q.All(ctx, db)))
}
func runAggregateNew(g *goldenCapture) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	a := rowset.As[expressionSide](models.Orders.Query(), "o")
	scope := rowset.Scope(a)
	amount := expr.OrderRow(expr.At(scope, models.Orders.TotalCents))
	buyer := expr.At(scope, models.Orders.BuyerID)
	count := expr.Count[rowset.Alias[expressionSide, models.Order]]().Filter(amount.Gt(0))
	total := expr.SumAt(scope, models.Orders.TotalCents).Filter(amount.Gt(100))
	s := aggregateBuyerRows
	q := rowset.Select(a, s, rowset.Map(s.Buyer, buyer), rowset.MapSelected(s.Count, count.Value()), rowset.MapSelected(s.Distinct, expr.CountDistinctFieldAt(scope, models.Orders.TotalCents).Value()), rowset.MapSelected(s.Total, total.Value()), rowset.MapSelected(s.Average, expr.AvgAt(scope, models.Orders.TotalCents).Value()), rowset.MapSelected(s.Min, expr.MinAt(scope, models.Orders.TotalCents).Value()), rowset.MapSelected(s.Max, expr.MaxAt(scope, models.Orders.TotalCents).Value()))
	q = rowset.OrderBy(rowset.Having(rowset.GroupBy(q, expr.Group(buyer)), expr.HavingAnd(count.Gt(0), expr.HavingOr(total.IsNull(), total.Gt(decimal.FromInt64(1))))), count.Desc(), buyer.Asc())
	got := must(q.All(ctx, db))
	// Owners differ between APIs; compare the same concrete printable row shape.
	normalized := make([]aggregateGoldenResult, len(got))
	for i, r := range got {
		normalized[i] = aggregateGoldenResult{r.Buyer.String(), r.Count, r.Distinct, r.Total, r.Average, r.Min, r.Max}
	}
	fmt.Fprintln(&g.output, normalized)
}
