package invalid

import "foundry.test/consumer/models"

var invalid = models.UserAggregates().OrderCount.Using(models.UserAggregates().OrderTotal)
