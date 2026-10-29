package row_owner

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type Other struct{}

var _ expr.RowExpression[Other, int] = expr.Row(expr.Value(orm.NumberExpr(models.Users.Age)))
