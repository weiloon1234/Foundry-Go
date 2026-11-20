package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func invalid() {
	users := expr.TextNullableValue(expr.AsSelected(expr.Row(expr.Value(models.Users.Nickname))))
	orders := expr.NullableTextValueComparison[models.Order, string](users)
	_ = expr.EqValue(orders, "x")
}
