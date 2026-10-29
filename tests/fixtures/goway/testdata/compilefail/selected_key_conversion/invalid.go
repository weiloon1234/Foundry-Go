package selected_key_conversion

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.ProjectionKey[models.Order](expr.Row(expr.Value(orm.NumberExpr(models.Users.Age))).Value().Key())
