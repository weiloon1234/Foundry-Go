package projection_aggregate_group

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.AsSelected(expr.Sum(models.Orders.TotalCents)).Group()
