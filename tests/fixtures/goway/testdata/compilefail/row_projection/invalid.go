package row_projection

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ expr.RowExpression[models.User, string] = expr.Row(expr.Value(models.Users.Nickname))
