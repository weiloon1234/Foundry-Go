package invalid

import (
	"foundry.test/consumer/models"
)

var _ = models.QueryOrders().Where(models.UserFields().Age.Param(2).Eq(2))
