package invalid

import (
	"foundry.test/goway/models"
)

var _ = models.UserRelations.Orders.Limit(1)
