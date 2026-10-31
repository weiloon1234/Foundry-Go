package case_unfinished

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ expr.Selected[models.User, string] = expr.When(expr.Gt(expr.OrderRow(expr.Value(orm.NumberExpr(models.Users.Age))), 0), expr.Value(orm.TextExpr(models.Users.Email)))
