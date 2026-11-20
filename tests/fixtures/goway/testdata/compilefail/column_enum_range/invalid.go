package column_enum_range

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var age = expr.Value(models.Users.Age)
var email = expr.Value(models.Users.Email)
var _ = expr.GreaterColumn[models.User, models.Status, models.Status](models.Users.Status, models.Users.Status)
