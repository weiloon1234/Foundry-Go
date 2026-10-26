package value_compare_selected_owner

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var age = expr.Value(models.Users.Age)
var email = expr.Value(models.Users.Email)

type otherSide struct{}

var other = rows.As[otherSide](models.Users.Query(), "other")
var _ = expr.EqualValue(expr.Row(age).Value(), expr.Row(expr.At(rows.Scope(other), models.Users.Age)).Value())
