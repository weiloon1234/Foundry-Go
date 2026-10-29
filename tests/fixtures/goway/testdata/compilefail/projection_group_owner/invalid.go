package projection_group_owner

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.GroupBy(rows.SelectValue(models.Users.Query(), orm.TextExpr(models.Users.Email)), expr.Group(expr.Value(orm.ColumnExpr(models.Orders.BuyerID))))
