package required_null_test

import (
	"foundry.test/goway/models"
)

func F() { models.Orders.TotalCents.IsNull() }
