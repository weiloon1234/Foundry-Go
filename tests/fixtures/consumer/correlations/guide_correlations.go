// Package correlations exercises typed correlated subqueries.
package correlations

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// BuyerOrderQueries builds the guide's correlated sources and the per-user report.
func BuyerOrderQueries() (rows.CorrelatedSource[models.User, models.Order], rows.CorrelatedSource[models.User, models.Order], expr.Scope[rows.Correlation[models.User, models.Order], models.User], expr.Scope[rows.Correlation[models.User, models.Order], models.Order], rows.CorrelatedScalar[models.User, models.Order, int64], rows.CorrelatedScalar[models.User, models.Order, value.Nullable[decimal.Decimal]], rows.Query[models.User, reports.UserOrderStats]) {
	// doc-example:begin model-correlations
	link := rows.Correlate(rows.From(models.Users.Query()), rows.From(models.Orders.Query()))
	buyer := rows.OuterScope(link, rows.ModelScope(models.Users.Query()))
	order := rows.InnerScope(link, rows.ModelScope(models.Orders.Query()))
	matched := link.Where(expr.Equal(expr.At(order, models.Orders.BuyerID), expr.At(buyer, models.Users.ID)))
	counts := rows.SelectCorrelatedScalar(matched, expr.AsSelected(expr.CountFieldAt(order, models.Orders.ID)))
	totals := rows.SelectCorrelatedScalar(matched, expr.AsSelected(expr.SumAt(order, models.Orders.TotalCents)))
	// doc-example:end model-correlations
	// doc-example:begin embed-correlated-scalar
	u := models.Users
	r := reports.UserOrderStatsRows
	report := rows.OrderBy(rows.Select(rows.From(models.Users.Query()), r.Row,
		rows.Map(r.ID, expr.Value(u.ID)), rows.Map(r.Email, expr.Value(u.Email)),
		rows.MapSelected(r.Orders, expr.AsSelected(rows.CorrelatedScalarRowQuery(counts))),
		rows.MapSelected(r.Total, expr.AsSelected(rows.CorrelatedScalarNullableRowQuery(totals)))), expr.Asc(expr.Value(u.Email)))
	// doc-example:end embed-correlated-scalar
	return link, matched, buyer, order, counts, totals, report
}
