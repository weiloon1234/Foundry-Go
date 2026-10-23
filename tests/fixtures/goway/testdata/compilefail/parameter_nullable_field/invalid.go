package parameter_nullable_field

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/value"
)

var age = expr.Value(models.Users.Age)
var email = expr.Value(models.Users.Email)
var _ = expr.Param(models.Users.Nickname, value.Of("name"))
