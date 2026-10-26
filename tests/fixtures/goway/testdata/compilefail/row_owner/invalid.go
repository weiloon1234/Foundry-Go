package row_owner

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type Other struct{}

var _ expr.RowExpression[Other, int] = expr.Row(expr.Value(models.Users.Age))
