package projection_order_scope

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.OrderBy(rows.SelectValue(models.Orders.Query(), orm.NumberExpr(models.Orders.TotalCents)), expr.Count[models.User]().Desc())
