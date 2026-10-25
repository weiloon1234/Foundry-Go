package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var invalid = expr.Exists[models.Order]().Filter(expr.ModelPredicate(models.Orders.TotalCents.Gt(0))).Gt(true)
