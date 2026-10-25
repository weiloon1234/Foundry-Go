package setqueries_test

import (
	"testing"

	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestCompileCombinedReportExample(t *testing.T) {
	source := derivedReports()
	total := rows.At(source.Scope(), reports.BuyerTotalRows.Total)
	r := reports.OrderSummaries
	selected := rows.Select(source, r.Row,
		rows.MapSelected(r.Total, expr.SumNullableRow(total).Value()),
		rows.MapSelected(r.Count, expr.Count[rows.Set[reports.BuyerTotals]]().Value()))
	if _, err := rows.CompileRead(rows.Read(selected)); err != nil {
		t.Fatal(err)
	}
}

func derivedReports() rows.SetQuery[reports.BuyerTotals] {
	u, o, r := models.Users, models.Orders, reports.BuyerTotalRows
	// Both reports return BuyerTotals while selecting from different model scopes.
	orders := rows.GroupBy(rows.Select(rows.From(o.Query()), r.Row,
		rows.Map(r.BuyerID, expr.Value(o.BuyerID)), rows.MapSelected(r.Total, expr.Sum(o.TotalCents).Value()),
		rows.MapSelected(r.Orders, expr.Count[models.Order]().Value()), rows.MapSelected(r.Average, expr.Avg(o.TotalCents).Value())), expr.Group(expr.Value(o.BuyerID)))
	people := rows.GroupBy(rows.Select(rows.From(u.Query()), r.Row,
		rows.Map(r.BuyerID, expr.Value(u.ID)), rows.MapSelected(r.Total, expr.Sum(u.Age).Value()),
		rows.MapSelected(r.Orders, expr.Count[models.User]().Value()), rows.MapSelected(r.Average, expr.Avg(u.Age).Value())), expr.Group(expr.Value(u.ID)))
	return rows.UnionAll(rows.RecordOf(orders), rows.RecordOf(people))
}
