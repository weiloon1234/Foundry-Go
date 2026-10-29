package parameter_owner

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var age = expr.Value(orm.NumberExpr(models.Users.Age))
var email = expr.Value(orm.TextExpr(models.Users.Email))
var _ = expr.Where(models.Orders.Query(), expr.Param(orm.NumberExpr(models.Users.Age), 2).Eq(2))
