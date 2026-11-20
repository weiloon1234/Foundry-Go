package aggregate_filter_having

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Filter(expr.Sum(models.Orders.TotalCents), expr.GtValue(expr.CountField(models.Orders.TotalCents), 0))
