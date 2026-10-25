package invalid

import "foundry.test/consumer/models"

var invalid = models.Users.Query().With(models.OrderLinks.Buyer)
