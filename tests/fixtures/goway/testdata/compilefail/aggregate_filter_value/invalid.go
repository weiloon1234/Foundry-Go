package aggregate_filter_value

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Sum(models.Orders.TotalCents).Filter(expr.OrderRow(expr.Value(models.Orders.TotalCents)).Gt("1"))
