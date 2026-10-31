package distinct

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func Query() rows.Query[models.Order, models.Order] {
	buyer := expr.Value(orm.ColumnExpr(models.Orders.BuyerID))
	return rows.OrderBy(rows.DistinctOn(rows.FromModel(models.Orders.Query()), expr.Group(buyer)), expr.Asc(buyer), expr.Desc(expr.Value(orm.KeyExpr(models.Orders.ID))))
}
func Values() rows.Query[models.Order, int64] {
	q := rows.SelectValue(models.Orders.Query(), orm.NumberExpr(models.Orders.TotalCents))
	return rows.DistinctOnValues(rows.Distinct(q), expr.Key(expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(models.Orders.TotalCents))))))
}
