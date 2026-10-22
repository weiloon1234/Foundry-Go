package wrong_predicate

import (
	"foundry.test/goway/models"
)

func F() { models.Orders.Query().Where(models.Users.Age.Gt(1)) }
