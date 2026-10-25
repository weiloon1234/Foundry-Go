package setqueries_test

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"testing"
)

func checkDerivedSetAggregation(t *testing.T, tx *database.Tx) error {
	reportsQuery := derivedReports()
	r := reports.BuyerTotalRows
	total := rows.At(reportsQuery.Scope(), r.Total)
	results, err := reportsQuery.Where(expr.OrderNullableRow(total).Gte(decimal.FromInt64(20))).OrderBy(total.Asc()).All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(results) != 3 {
		t.Fatal("set of different input scopes lost output filtering")
	}
	if amount, ok := results[0].Total.Get(); !ok || amount.String() != "20" {
		t.Error("set changed exact decimal codec")
	}
	// Project and group the combined result through the same projection runtime.
	s := reports.OrderSummaries
	summary := rows.Select(reportsQuery, s.Row,
		rows.MapSelected(s.Total, expr.SumNullableRow(total).Value()),
		rows.MapSelected(s.Count, expr.Count[rows.Set[reports.BuyerTotals]]().Value()))
	row, err := rows.Read(summary).RequireFirst(t.Context(), tx)
	if err != nil {
		return err
	}
	if row.Count != 5 {
		t.Error("projection over set lost rows")
	}
	if total, ok := row.Total.Get(); !ok || total.String() != "69" {
		t.Error("projection over set lost exact aggregate")
	}
	return nil
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
