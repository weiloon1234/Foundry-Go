package setqueries_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

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
		rows.MapSelected(r.Total, expr.AsSelected(expr.SumNullableRow(total))),
		rows.MapSelected(r.Count, expr.AsSelected(expr.Count[rows.Set[reports.BuyerTotals]]())))
	if _, err := rows.CompileRead(rows.Read(selected)); err != nil {
		t.Fatal(err)
	}
}

func derivedReports() rows.SetQuery[reports.BuyerTotals] {
	u, o, r := models.Users, models.Orders, reports.BuyerTotalRows
	// Both reports return BuyerTotals while selecting from different model scopes.
	orders := rows.GroupBy(rows.Select(rows.From(o.Query()), r.Row,
		rows.Map(r.BuyerID, expr.Value(orm.ColumnExpr(o.BuyerID))), rows.MapSelected(r.Total, expr.AsSelected(expr.Sum(orm.NumberExpr(o.TotalCents)))),
		rows.MapSelected(r.Orders, expr.AsSelected(expr.Count[models.Order]())), rows.MapSelected(r.Average, expr.AsSelected(expr.Avg(orm.NumberExpr(o.TotalCents))))), expr.Group(expr.Value(orm.ColumnExpr(o.BuyerID))))
	people := rows.GroupBy(rows.Select(rows.From(u.Query()), r.Row,
		rows.Map(r.BuyerID, expr.Value(orm.KeyExpr(u.ID))), rows.MapSelected(r.Total, expr.AsSelected(expr.Sum(orm.NumberExpr(u.Age)))),
		rows.MapSelected(r.Orders, expr.AsSelected(expr.Count[models.User]())), rows.MapSelected(r.Average, expr.AsSelected(expr.Avg(orm.NumberExpr(u.Age))))), expr.Group(expr.Value(orm.KeyExpr(u.ID))))
	return rows.UnionAll(rows.RecordOf(orders), rows.RecordOf(people))
}
