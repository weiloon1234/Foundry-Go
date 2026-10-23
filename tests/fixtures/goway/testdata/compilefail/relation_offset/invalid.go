package invalid

import (
	"foundry.test/goway/models"
)

var _ = models.UserRelations.Orders.Offset(1)
