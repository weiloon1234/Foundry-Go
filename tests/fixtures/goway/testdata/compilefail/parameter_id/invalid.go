package parameter_id

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/model"
)

var age = expr.Value(orm.NumberExpr(models.Users.Age))
var email = expr.Value(orm.TextExpr(models.Users.Email))
var _ = expr.Param(orm.KeyExpr(models.Users.ID), model.ID[models.Order]{})
