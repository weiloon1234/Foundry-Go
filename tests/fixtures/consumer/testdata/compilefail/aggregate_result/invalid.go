package invalid

import "foundry.test/consumer/models"

var invalid = models.UserValues.OrderCount.Using(models.UserValues.OrderTotal)
