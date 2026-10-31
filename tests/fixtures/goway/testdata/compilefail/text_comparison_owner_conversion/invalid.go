package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func invalid() {
	users := expr.TextValue(expr.AsSelected(expr.Row(expr.Value(orm.TextExpr(models.Users.Email)))))
	orders := expr.TextValueComparison[models.Order, string](users)
	_ = expr.EqValue(orders, "x")
}
