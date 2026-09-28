package invalid

import "foundry.test/consumer/models"

var invalid = models.QueryUsers().WhereHas(models.UserAggregates().OrderCount)
