package aggregate_group

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
	buyer := expr.Value(models.Orders.BuyerID)
	total := expr.Sum(models.Orders.TotalCents).Filter(expr.OrderRow(expr.Value(models.Orders.TotalCents)).Gt(0))
	count := expr.CountField(models.Orders.TotalCents)
	q := rows.Select(rows.FromModel(models.Orders.Query()), schema, rows.Map(schema.Buyer, buyer), rows.MapSelected(schema.Count, count.Value()), rows.MapSelected(schema.Distinct, expr.CountDistinctField(models.Orders.TotalCents).Value()), rows.MapSelected(schema.Total, total.Value()), rows.MapSelected(schema.Minimum, expr.Min(models.Orders.TotalCents).Value()))
	return rows.OrderBy(rows.Having(rows.GroupBy(q, expr.Group(buyer)), expr.HavingAnd(count.Gt(0), total.IsNotNull())), count.Desc(), total.Asc().NullsLast(), buyer.Asc())
}
