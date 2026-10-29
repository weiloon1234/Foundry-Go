package arithmetic_nullable_operand

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var age = expr.Value(orm.NumberExpr(models.Users.Age))
var email = expr.Value(orm.TextExpr(models.Users.Email))
var _ = expr.AddNullable(expr.NullableRow(age), age)
