package reports

import (
	"context"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
)

// doc-example:begin model-projections
func BuyerTotalsByBuyer() rows.Query[models.Order, BuyerTotals] {
	o := models.Orders
	return rows.GroupBy(rows.Select(rows.From(models.Orders.Query()), BuyerTotalRows.Row,
		rows.Map(BuyerTotalRows.BuyerID, expr.Value(o.BuyerID)),
		rows.MapSelected(BuyerTotalRows.Total, expr.AsSelected(expr.Sum(o.TotalCents))),
		rows.MapSelected(BuyerTotalRows.Orders, expr.AsSelected(expr.Count[models.Order]())),
		rows.MapSelected(BuyerTotalRows.Average, expr.AsSelected(expr.Avg(o.TotalCents))),
	), expr.Group(expr.Value(o.BuyerID)))
}

// doc-example:end model-projections

// doc-example:begin read-scalar
func OrderedEmails(ctx context.Context, db database.Executor) ([]string, error) {
	users := models.Users
	emails := rows.SelectScalar(rows.From(users.Query()), expr.AsSelected(expr.Row(expr.Value(users.Email))))
	return rows.ReadScalar(rows.OrderScalar(emails, expr.Asc(expr.Value(users.Email)))).All(ctx, db)
}

// doc-example:end read-scalar

// doc-example:begin scalar-projection
func ScalarSummaryQuery(buyers rows.Scalar[models.Order, model.ID[models.User]], count rows.Scalar[models.Order, int64], nicknameOf model.ID[models.User]) rows.Query[models.User, ScalarReport] {
	u, o := models.Users, models.Orders
	last := rows.LimitScalar(rows.OrderScalar(buyers, expr.Desc(expr.Value(o.TotalCents))), 1)
	nicknames := rows.SelectScalar(rows.From(models.Users.Query().Where(orm.Eq(u.ID, nicknameOf))), expr.AsSelected(expr.Row(expr.Value(u.Nickname))))
	return rows.OrderBy(rows.Select(rows.From(models.Users.Query()), ScalarReports.Row,
		rows.Map(ScalarReports.ID, expr.Value(u.ID)),
		rows.MapSelected(ScalarReports.BuyerID, expr.AsSelected(rows.ScalarRowQuery(rows.From(models.Users.Query()), last))),
		rows.MapSelected(ScalarReports.Nickname, expr.AsSelected(rows.ScalarNullableRowQuery(rows.From(models.Users.Query()), nicknames))),
		rows.MapSelected(ScalarReports.Orders, expr.AsSelected(rows.ScalarRowQuery(rows.From(models.Users.Query()), count))),
	), expr.Asc(expr.Value(u.Email)))
}

// doc-example:end scalar-projection

// doc-example:begin model-aggregate-filters
func FilteredOrderSummary() rows.Query[models.Order, OrderSummary] {
	predicate := expr.ModelPredicate(orm.Gt(models.Orders.TotalCents, 0))
	total := expr.Filter(expr.Sum(models.Orders.TotalCents), predicate)
	count := expr.Filter(expr.Count[models.Order](), predicate)
	exists := expr.Filter(expr.Exists[models.Order](), predicate)
	selected := rows.Select(rows.From(models.Orders.Query()), OrderSummaries,
		rows.MapSelected(OrderSummaries.Total, expr.AsSelected(total)),
		rows.MapSelected(OrderSummaries.Count, expr.AsSelected(count)))
	return rows.Having(selected, expr.HavingAnd(expr.GtValue(count, 0), expr.GtValue(total, decimal.FromInt64(1)), expr.EqValue(exists, true)))
}

// doc-example:end model-aggregate-filters

// doc-example:begin model-set-operations
func DerivedReports() rows.SetQuery[BuyerTotals] {
	u, o, r := models.Users, models.Orders, BuyerTotalRows
	// Both reports return BuyerTotals while selecting from different model scopes.
	orders := rows.GroupBy(rows.Select(rows.From(o.Query()), r.Row,
		rows.Map(r.BuyerID, expr.Value(o.BuyerID)), rows.MapSelected(r.Total, expr.AsSelected(expr.Sum(o.TotalCents))),
		rows.MapSelected(r.Orders, expr.AsSelected(expr.Count[models.Order]())), rows.MapSelected(r.Average, expr.AsSelected(expr.Avg(o.TotalCents)))), expr.Group(expr.Value(o.BuyerID)))
	people := rows.GroupBy(rows.Select(rows.From(u.Query()), r.Row,
		rows.Map(r.BuyerID, expr.Value(u.ID)), rows.MapSelected(r.Total, expr.AsSelected(expr.Sum(u.Age))),
		rows.MapSelected(r.Orders, expr.AsSelected(expr.Count[models.User]())), rows.MapSelected(r.Average, expr.AsSelected(expr.Avg(u.Age)))), expr.Group(expr.Value(u.ID)))
	return rows.UnionAll(rows.RecordOf(orders), rows.RecordOf(people))
}

// doc-example:end model-set-operations
