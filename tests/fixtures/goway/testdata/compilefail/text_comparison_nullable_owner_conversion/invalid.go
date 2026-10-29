package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func invalid() {
	users := expr.TextNullableValue(expr.Row(expr.Value(orm.NullableTextExpr(models.Users.Nickname))).Value())
	orders := expr.NullableTextValueComparison[models.Order, string](users)
	_ = orders.Eq("x")
}
