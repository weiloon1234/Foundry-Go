package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.RowExpression[models.User, models.Status](expr.AsSelected(expr.CompareRow(expr.Value(orm.EnumExpr(models.Users.Status)))))
