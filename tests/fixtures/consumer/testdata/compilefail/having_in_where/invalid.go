package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var invalid = expr.Where(models.Orders.Query(), expr.GtValue(expr.Count[models.Order](), 1))
