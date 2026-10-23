package case_unfinished

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ expr.Selected[models.User, string] = expr.When(expr.OrderRow(expr.Value(models.Users.Age)).Gt(0), expr.Value(models.Users.Email))
