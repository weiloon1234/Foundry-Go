package invalid

import "foundry.test/consumer/models"

var invalid = models.QueryUsers().With(models.OrderRelations().Buyer)
