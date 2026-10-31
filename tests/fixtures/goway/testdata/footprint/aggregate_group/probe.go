package aggregate_group

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Result struct {
	Buyer           model.ID[models.User]
	Count, Distinct int64
	Total           value.Nullable[decimal.Decimal]
	Minimum         value.Nullable[int64]
}
type fields struct {
	rows.Row[Result]
	Buyer           rows.Field[Result, model.ID[models.User]]
	Count, Distinct rows.Field[Result, int64]
	Total           rows.Field[Result, value.Nullable[decimal.Decimal]]
	Minimum         rows.Field[Result, value.Nullable[int64]]
}

var schema = rows.Define(func(b rows.Builder[Result], r *Result) fields {
	return fields{b.Row(), rows.IDOf(b, &r.Buyer), rows.NumberOf(b, &r.Count), rows.NumberOf(b, &r.Distinct), rows.NullableNumberOf(b, &r.Total), rows.NullableNumberOf(b, &r.Minimum)}
})

func Query() rows.Query[models.Order, Result] {
	buyer := expr.Value(orm.ColumnExpr(models.Orders.BuyerID))
	total := expr.Filter(expr.Sum(orm.NumberExpr(models.Orders.TotalCents)), expr.Gt(expr.OrderRow(expr.Value(orm.NumberExpr(models.Orders.TotalCents))), 0))
	count := expr.CountField(orm.NumberExpr(models.Orders.TotalCents))
	q := rows.Select(rows.FromModel(models.Orders.Query()), schema, rows.Map(schema.Buyer, buyer), rows.MapSelected(schema.Count, expr.AsSelected(count)), rows.MapSelected(schema.Distinct, expr.AsSelected(expr.CountDistinctField(orm.NumberExpr(models.Orders.TotalCents)))), rows.MapSelected(schema.Total, expr.AsSelected(total)), rows.MapSelected(schema.Minimum, expr.AsSelected(expr.Min(orm.NumberExpr(models.Orders.TotalCents)))))
	return rows.OrderBy(rows.Having(rows.GroupBy(q, expr.Group(buyer)), expr.HavingAnd(expr.GtValue(count, 0), expr.IsNotNullValue(total))), expr.DescValue(count), expr.AscValue(total).NullsLast(), expr.Asc(buyer))
}
