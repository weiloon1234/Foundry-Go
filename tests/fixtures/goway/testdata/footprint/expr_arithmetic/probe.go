package expr_arithmetic

import (
	"github.com/weiloon1234/Foundry-Go/value"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

// Query exercises typed row calculations without importing the projection family.
func Query(v int64) orm.Query[models.Order] {
	amount := expr.Value(orm.NumberExpr(models.Orders.TotalCents))
	doubled := expr.Add(amount, expr.Param(orm.NumberExpr(models.Orders.TotalCents), v))
	nullable := expr.AddNullable(expr.NullableRow(amount), expr.NullableRow(amount).Param(value.Null[int64]()))
	return expr.OrderBy(expr.Where(models.Orders.Query(), doubled.Gt(v), nullable.IsNull(), expr.Equal(amount, doubled.Param(v))), expr.Negate(doubled).Asc())
}
