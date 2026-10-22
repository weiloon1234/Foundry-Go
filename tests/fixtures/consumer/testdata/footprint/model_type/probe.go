package model_type

import "foundry.test/consumer/models"

func Total(o models.Order) int64 { return o.TotalCents }
