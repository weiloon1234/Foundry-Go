package model_type

import (
	"foundry.test/goway/models"
)

func F(o models.Order) int64 { return o.TotalCents }
