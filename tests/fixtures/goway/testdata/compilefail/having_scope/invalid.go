package having_scope

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.Having(rows.SelectValue(models.Orders.Query(), orm.NumberExpr(models.Orders.TotalCents)), expr.GtValue(expr.Count[models.User](), 1))
