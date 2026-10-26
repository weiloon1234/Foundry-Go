package distinct

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func Query() rows.Query[models.Order, models.Order] {
	buyer := expr.Value(models.Orders.BuyerID)
	return rows.DistinctOn(rows.FromModel(models.Orders.Query()), expr.Group(buyer)).OrderBy(buyer.Asc(), expr.Value(models.Orders.ID).Desc())
}
func Values() rows.Query[models.Order, int64] {
	q := rows.SelectValue(models.Orders.Query(), models.Orders.TotalCents)
	return rows.DistinctOnValues(rows.Distinct(q), expr.Row(expr.Value(models.Orders.TotalCents)).Value().Key())
}
