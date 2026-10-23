package invalid

import (
	"foundry.test/goway/models"
)

var _ = models.UserRelations.Orders.With(models.UserRelations.Introducer)
