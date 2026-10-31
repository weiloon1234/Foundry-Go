package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var invalid = expr.GtValue(expr.Filter(expr.Exists[models.Order](), expr.ModelPredicate(orm.NumberGt(models.Orders.TotalCents, 0))), true)
