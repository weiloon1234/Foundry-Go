package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var invalid = expr.Exists[models.Order]().Filter(expr.ModelPredicate(orm.NumberGt(models.Orders.TotalCents, 0))).Gt(true)
