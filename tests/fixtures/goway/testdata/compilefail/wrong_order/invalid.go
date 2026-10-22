package wrong_order

import (
	"foundry.test/goway/models"
)

func F() { models.Orders.Query().OrderBy(models.Users.Age.Desc()) }
