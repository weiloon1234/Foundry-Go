package coalesce_value

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Coalesce(expr.Value(models.Users.Nickname), expr.Value(models.Users.Age))
