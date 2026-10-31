package aggregate_order_model

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.OrderBy(models.Orders.Query(), expr.DescValue(expr.Count[models.Order]()))
