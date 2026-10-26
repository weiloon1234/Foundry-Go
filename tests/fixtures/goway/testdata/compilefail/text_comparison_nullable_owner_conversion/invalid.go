package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func invalid() {
	users := expr.TextNullableValue(expr.Row(expr.Value(models.Users.Nickname)).Value())
	orders := expr.NullableTextValueComparison[models.Order, string](users)
	_ = orders.Eq("x")
}
