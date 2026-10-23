package aggregate_filter_having

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Sum(models.Orders.TotalCents).Filter(expr.CountField(models.Orders.TotalCents).Gt(0))
