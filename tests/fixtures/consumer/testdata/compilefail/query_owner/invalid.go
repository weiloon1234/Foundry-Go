package invalid

import "foundry.test/consumer/models"

var invalid = models.Users.Query().Where(models.Orders.TotalCents.Eq(10))
