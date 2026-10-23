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

type correlationGoldenRow struct{ Amount value.Nullable[int64] }
type correlationGoldenFields struct {
	rowset.Row[correlationGoldenRow]
	Amount rowset.Field[correlationGoldenRow, value.Nullable[int64]]
}

var correlationGoldenRows = rowset.Define(func(b rowset.Builder[correlationGoldenRow], r *correlationGoldenRow) correlationGoldenFields {
	return correlationGoldenFields{b.Row(), rowset.NullableNumberOf(b, &r.Amount)}
})

func oldCorrelationProjection[S any](source query.ProjectionSource[S], v query.Expression[S, value.Nullable[int64]]) query.ProjectionQuery[S, correlationGoldenRow] {
	field := query.NewProjectionField[correlationGoldenRow, value.Nullable[int64]]("amount")
	def := query.DefineProjection([]query.ProjectionColumn[correlationGoldenRow]{field.Column()}, func(row database.Row) (correlationGoldenRow, error) {
		var r correlationGoldenRow
		err := row.Scan(codec.Nullable(codec.Signed[int64]()).Scan(&r.Amount))
		return r, err
	})
	return query.Project(source, def, query.Map(field, v))
}

func TestStage6b3CorrelationAndLateralGolden(t *testing.T) {
	for _, kind := range []string{"correlation", "nested", "cross", "inner", "left", "on"} {
		t.Run(kind, func(t *testing.T) {
			before, after := &goldenCapture{}, &goldenCapture{}
			for _, g := range []*goldenCapture{before, after} {
				g.scriptedColumns = []string{"amount"}
				g.scriptedBatches = [][][]driver.Value{{{int64(2)}, {nil}}}
			}
			runCorrelationOld(before, kind)
			runCorrelationNew(after, kind)
			if before.statements != 1 || after.statements != 1 || before.output.String() != after.output.String() {
				t.Fatalf("correlation/lateral parity:\n%s", goldenDiff(before.output.String(), after.output.String()))
			}
		})
	}
}

func runCorrelationOld(g *goldenCapture, kind string) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	if kind == "correlation" {
		u, o := old.QueryUsers().Where(old.UserFields().Age.Gt(18)), old.QueryOrders().Where(old.OrderFields().TotalCents.Gt(5))
		c := query.Correlate(u, o)
		uf, of := old.UserFieldsAt(query.OuterScope(c, u.Scope())), old.OrderFieldsAt(query.InnerScope(c, o.Scope()))
		c = c.Where(of.BuyerID.EqColumn(uf.ID))
		count := query.SelectCorrelatedValue(c, query.Count[query.Correlation[old.User, old.Order]]().Value())
		ids := query.SelectCorrelatedValue(c, query.Value(of.BuyerID))
		got := must(oldCorrelationProjection(u, query.CorrelatedScalarQuery(count)).Where(c.Exists(), old.UserFields().ID.InCorrelatedQuery(ids)).All(ctx, db))
		fmt.Fprintln(&g.output, got)
		return
	}
	a, b := query.As[joinA](old.QueryUsers(), "a"), query.As[joinB](old.QueryOrders(), "b")
	c := query.Correlate(a, b)
	uf, of := old.UserFieldsAt(query.OuterScope(c, a.Scope())), old.OrderFieldsAt(query.InnerScope(c, b.Scope()))
	c = c.Where(of.BuyerID.EqColumn(uf.ID))
	if kind == "nested" {
		d := query.As[joinC](old.QueryOrders(), "d")
		nested := query.Correlate(c, d)
		outer := old.UserFieldsAt(query.OuterScope(nested, query.OuterScope(c, a.Scope())))
		local := old.OrderFieldsAt(query.InnerScope(nested, d.Scope()))
		c = c.Where(nested.Where(local.BuyerID.EqColumn(outer.ID)).Limit(1).Exists())
		count := query.SelectCorrelatedValue(c, query.Count[query.Correlation[query.Alias[joinA, old.User], query.Alias[joinB, old.Order]]]().Value())
		fmt.Fprintln(&g.output, must(oldCorrelationProjection(a, query.CorrelatedScalarQuery(count)).All(ctx, db)))
		return
	}
	record := query.SelectCorrelatedRecord(c, query.InnerScope(c, b.Scope())).OrderBy(of.TotalCents.Desc()).Limit(1)
	latest := query.AsLateral[joinC](record, "latest")
	switch kind {
	case "cross":
		j := query.CrossJoinLateral(a, latest)
		f := old.OrderFieldsAt(query.RightScope(j, latest.Scope()))
		fmt.Fprintln(&g.output, must(oldCorrelationProjection(j, query.Nullable(query.Value(f.TotalCents))).All(ctx, db)))
	case "inner":
		j := query.InnerJoinLateral(a, latest)
		f := old.OrderFieldsAt(query.RightScope(j, latest.Scope()))
		fmt.Fprintln(&g.output, must(oldCorrelationProjection(j, query.Nullable(query.Value(f.TotalCents))).All(ctx, db)))
	case "left":
		j := query.LeftJoinLateral(a, latest)
		f := old.OrderNullableFieldsAt(query.NullableRightScope(j, latest.Scope()))
		fmt.Fprintln(&g.output, must(oldCorrelationProjection(j, query.Value(f.TotalCents)).All(ctx, db)))
	case "on":
		on := query.On(old.UserFieldsAt(a.Scope()).ID, old.OrderFieldsAt(latest.Scope()).BuyerID)
		j := query.LeftJoinLateral(a, latest, on)
		f := old.OrderNullableFieldsAt(query.NullableRightScope(j, latest.Scope()))
		fmt.Fprintln(&g.output, must(oldCorrelationProjection(j, query.Value(f.TotalCents)).All(ctx, db)))
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
		ids := rowset.SelectCorrelatedScalar(c, expr.Select(buyer))
		q := rowset.Select(outer, correlationGoldenRows, rowset.MapSelected(correlationGoldenRows.Amount, rowset.CorrelatedScalarQuery(count))).Where(c.Exists(), rowset.InCorrelatedQuery(expr.Value(models.Users.ID), ids))
		fmt.Fprintln(&g.output, must(q.All(ctx, db)))
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
		fmt.Fprintln(&g.output, must(rowset.Select(a, correlationGoldenRows, rowset.MapSelected(correlationGoldenRows.Amount, rowset.CorrelatedScalarQuery(count))).All(ctx, db)))
		return
	}
	record := rowset.SelectCorrelatedRecord(c, inner).OrderBy(expr.At(inner, models.Orders.TotalCents).Desc()).Limit(1)
	latest := rowset.AsLateral[joinC](record, "latest")
	switch kind {
	case "cross":
		j := rowset.CrossJoinLateral(a, latest)
		v := expr.At(rowset.RightScope(j, rowset.LateralScope(latest)), models.Orders.TotalCents)
		fmt.Fprintln(&g.output, must(rowset.Select(j, correlationGoldenRows, rowset.MapSelected(correlationGoldenRows.Amount, expr.Nullable(expr.Select(v)))).All(ctx, db)))
	case "inner":
		j := rowset.InnerJoinLateral(a, latest)
		v := expr.At(rowset.RightScope(j, rowset.LateralScope(latest)), models.Orders.TotalCents)
		fmt.Fprintln(&g.output, must(rowset.Select(j, correlationGoldenRows, rowset.MapSelected(correlationGoldenRows.Amount, expr.Nullable(expr.Select(v)))).All(ctx, db)))
	case "left":
		j := rowset.LeftJoinLateral(a, latest)
		v := expr.AtNullable(rowset.NullableRightScope(j, rowset.LateralScope(latest)), models.Orders.TotalCents)
		fmt.Fprintln(&g.output, must(rowset.Select(j, correlationGoldenRows, rowset.Map(correlationGoldenRows.Amount, v)).All(ctx, db)))
	case "on":
		on := rowset.On(expr.At(rowset.Scope(a), models.Users.ID), expr.At(rowset.LateralScope(latest), models.Orders.BuyerID))
		j := rowset.LeftJoinLateral(a, latest, on)
		v := expr.AtNullable(rowset.NullableRightScope(j, rowset.LateralScope(latest)), models.Orders.TotalCents)
		fmt.Fprintln(&g.output, must(rowset.Select(j, correlationGoldenRows, rowset.Map(correlationGoldenRows.Amount, v)).All(ctx, db)))
	}
}
