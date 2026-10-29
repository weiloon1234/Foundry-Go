package row_projection

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ expr.RowExpression[models.User, string] = expr.Row(expr.Value(orm.NullableTextExpr(models.Users.Nickname)))
