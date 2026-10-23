package aggregate_filter_boolean_order

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Exists[models.Order]().Filter(expr.Value(models.Orders.TotalCents).Eq(0)).Gt(true)
