package invalid

import "foundry.test/consumer/models"

var invalid = models.QueryUsers().WhereDoesntHave(models.OrderRelations().Buyer)
