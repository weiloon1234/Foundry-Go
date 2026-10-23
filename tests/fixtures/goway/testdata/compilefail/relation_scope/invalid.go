package invalid

import (
	"foundry.test/goway/models"
)

var _ = models.UserRelations.Orders.Where(models.Users.Age.Gt(18))
