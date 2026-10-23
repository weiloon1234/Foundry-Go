package invalid

import (
	"foundry.test/goway/models"
)

var _ = models.OrderRelations.Buyer.Where(models.Orders.TotalCents.Gt(1))
