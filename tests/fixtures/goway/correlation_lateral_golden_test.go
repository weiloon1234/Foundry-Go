package goway_test

import (
	"context"
	"database/sql/driver"
	"fmt"
	"testing"

	"foundry.test/goway/models"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"github.com/weiloon1234/Foundry-Go/value"
)

type correlationGoldenRow struct{ Amount value.Nullable[int64] }
type correlationGoldenFields struct {
	rowset.Row[correlationGoldenRow]
	Amount rowset.Field[correlationGoldenRow, value.Nullable[int64]]
}

var correlationGoldenRows = rowset.Define(func(b rowset.Builder[correlationGoldenRow], r *correlationGoldenRow) correlationGoldenFields {
	return correlationGoldenFields{b.Row(), rowset.NullableNumberOf(b, &r.Amount)}
})

func TestStage6b3CorrelationAndLateralGolden(t *testing.T) {
	for _, kind := range []string{"correlation", "nested", "cross", "inner", "left", "on"} {
		t.Run(kind, func(t *testing.T) {
			after := &goldenCapture{}
			for _, g := range []*goldenCapture{after} {
				g.scriptedColumns = []string{"amount"}
				g.scriptedBatches = [][][]driver.Value{{{int64(2)}, {nil}}}
			}

			runCorrelationNew(after, kind)
			if after.statements != 1 || !oracleEqual(t, "correlation_lateral_golden/1", after.output.String()) {
				t.Fatal("frozen legacy comparison differs")
			}
		})
	}
}

func runCorrelationNew(g *goldenCapture, kind string) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	if kind == "correlation" {
		u, o := models.Users.Query().Where(models.Users.Age.Gt(18)), models.Orders.Query().Where(models.Orders.TotalCents.Gt(5))
		outer := rowset.From(u)
		c := rowset.Correlate(outer, rowset.From(o))
		uid := expr.At(rowset.OuterScope(c, rowset.ModelScope(u)), models.Users.ID)
		buyer := expr.At(rowset.InnerScope(c, rowset.ModelScope(o)), models.Orders.BuyerID)
		c = c.Where(expr.Equal(buyer, uid))
		count := rowset.SelectCorrelatedScalar(c, expr.Count[rowset.Correlation[models.User, models.Order]]().Value())
		ids := rowset.SelectCorrelatedScalar(c, expr.Row(buyer).Value())
		q := rowset.Select(outer, correlationGoldenRows, rowset.MapSelected(correlationGoldenRows.Amount, rowset.CorrelatedScalarRowQuery(count).Value())).Where(c.Exists(), rowset.InCorrelatedQuery(expr.Value(models.Users.ID), ids))
		fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.Read(q).All(ctx, db))}))
		return
	}
	a, b := rowset.As[joinA](models.Users.Query(), "a"), rowset.As[joinB](models.Orders.Query(), "b")
	c := rowset.Correlate(a, b)
	uid := expr.At(rowset.OuterScope(c, rowset.Scope(a)), models.Users.ID)
	inner := rowset.InnerScope(c, rowset.Scope(b))
	buyer := expr.At(inner, models.Orders.BuyerID)
	c = c.Where(expr.Equal(buyer, uid))
	if kind == "nested" {
		d := rowset.As[joinC](models.Orders.Query(), "d")
		nested := rowset.Correlate(c, d)
		parent := expr.At(rowset.OuterScope(nested, rowset.OuterScope(c, rowset.Scope(a))), models.Users.ID)
		child := expr.At(rowset.InnerScope(nested, rowset.Scope(d)), models.Orders.BuyerID)
		c = c.Where(nested.Where(expr.Equal(child, parent)).Limit(1).Exists())
		count := rowset.SelectCorrelatedScalar(c, expr.Count[rowset.Correlation[rowset.Alias[joinA, models.User], rowset.Alias[joinB, models.Order]]]().Value())
		fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.Read(rowset.Select(a, correlationGoldenRows, rowset.MapSelected(correlationGoldenRows.Amount, rowset.CorrelatedScalarRowQuery(count).Value()))).All(ctx, db))}))
		return
	}
	record := rowset.SelectCorrelatedRecord(c, inner).OrderBy(expr.At(inner, models.Orders.TotalCents).Desc()).Limit(1)
	latest := rowset.AsLateral[joinC](record, "latest")
	switch kind {
	case "cross":
		j := rowset.CrossJoinLateral(a, latest)
		v := expr.At(rowset.RightScope(j, rowset.LateralScope(latest)), models.Orders.TotalCents)
		fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.Read(rowset.Select(j, correlationGoldenRows, rowset.MapSelected(correlationGoldenRows.Amount, expr.Nullable(expr.Row(v).Value())))).All(ctx, db))}))
	case "inner":
		j := rowset.InnerJoinLateral(a, latest)
		v := expr.At(rowset.RightScope(j, rowset.LateralScope(latest)), models.Orders.TotalCents)
		fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.Read(rowset.Select(j, correlationGoldenRows, rowset.MapSelected(correlationGoldenRows.Amount, expr.Nullable(expr.Row(v).Value())))).All(ctx, db))}))
	case "left":
		j := rowset.LeftJoinLateral(a, latest)
		v := expr.AtNullable(rowset.NullableRightScope(j, rowset.LateralScope(latest)), models.Orders.TotalCents)
		fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.Read(rowset.Select(j, correlationGoldenRows, rowset.Map(correlationGoldenRows.Amount, v))).All(ctx, db))}))
	case "on":
		on := rowset.On(expr.At(rowset.Scope(a), models.Users.ID), expr.At(rowset.LateralScope(latest), models.Orders.BuyerID))
		j := rowset.LeftJoinLateral(a, latest, on)
		v := expr.AtNullable(rowset.NullableRightScope(j, rowset.LateralScope(latest)), models.Orders.TotalCents)
		fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.Read(rowset.Select(j, correlationGoldenRows, rowset.Map(correlationGoldenRows.Amount, v))).All(ctx, db))}))
	}
}
