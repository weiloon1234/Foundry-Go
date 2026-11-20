package arithmetic_float_remainder

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var age = expr.Value(models.Users.Age)
var email = expr.Value(models.Users.Email)
var _ = expr.Remainder(expr.FloatOf(age), expr.FloatOf(age))
