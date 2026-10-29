package value_compare_selected_owner

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var age = expr.Value(orm.NumberExpr(models.Users.Age))
var email = expr.Value(orm.TextExpr(models.Users.Email))

type otherSide struct{}

var other = rows.As[otherSide](models.Users.Query(), "other")
var _ = expr.EqualValue(expr.Row(age).Value(), expr.Row(expr.At(rows.Scope(other), orm.NumberExpr(models.Users.Age))).Value())
