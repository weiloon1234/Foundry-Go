package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var invalid = expr.Filter(expr.Sum(models.Orders.TotalCents), expr.GtValue(expr.CountField(models.Orders.TotalCents), 0))
