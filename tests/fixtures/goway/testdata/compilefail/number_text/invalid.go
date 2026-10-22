package number_text

import (
	"foundry.test/goway/models"
)

func F() { models.Orders.TotalCents.Contains("x") }
