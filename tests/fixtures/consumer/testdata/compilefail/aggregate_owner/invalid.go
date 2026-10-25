package invalid

import "foundry.test/consumer/models"

var invalid = models.Orders.Query().With(models.UserValues.OrderCount)
