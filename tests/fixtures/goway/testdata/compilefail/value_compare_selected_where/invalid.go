package value_compare_selected_where

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var age = expr.Value(models.Users.Age)
var email = expr.Value(models.Users.Email)
var _ = expr.Where(models.Users.Query(), expr.EqualValue(expr.Row(age).Value(), expr.Row(age).Value()))
