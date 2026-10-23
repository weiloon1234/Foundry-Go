package value_compare_order_bool

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var age = expr.Value(models.Users.Age)
var email = expr.Value(models.Users.Email)
var _ = expr.Less(expr.Exists[models.User]().Param(true), expr.Exists[models.User]().Param(false))
