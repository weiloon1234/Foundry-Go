package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.RowExpression[models.User, models.Status](expr.CompareRow(expr.Value(models.Users.Status)).Value())
