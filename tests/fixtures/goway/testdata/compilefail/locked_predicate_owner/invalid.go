package invalid

import (
	"foundry.test/goway/models"
)

var _ = models.Users.Query().ForUpdate().Where(models.Orders.TotalCents.Gt(1))
