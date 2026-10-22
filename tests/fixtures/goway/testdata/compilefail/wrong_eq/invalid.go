package wrong_eq

import (
	"foundry.test/goway/models"
)

func F() { models.Orders.TotalCents.Eq("x") }
