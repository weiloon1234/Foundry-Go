package parameter_id

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/model"
)

var age = expr.Value(models.Users.Age)
var email = expr.Value(models.Users.Email)
var _ = expr.Param(models.Users.ID, model.ID[models.Order]{})
