package required_null_set

import (
	"foundry.test/goway/models"
)

func F() { models.Orders.TotalCents.SetNull() }
