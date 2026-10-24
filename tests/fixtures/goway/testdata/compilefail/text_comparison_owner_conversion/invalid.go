package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func invalid() {
	users := expr.TextValue(expr.Select(expr.Value(models.Users.Email)))
	orders := expr.TextValueComparison[models.Order, string](users)
	_ = orders.Eq("x")
}
