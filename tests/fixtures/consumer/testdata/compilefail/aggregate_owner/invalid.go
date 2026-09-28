package invalid

import "foundry.test/consumer/models"

var invalid = models.QueryOrders().With(models.UserAggregates().OrderCount)
