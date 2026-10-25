package invalid

import "foundry.test/consumer/models"

var invalid = models.UserLinks.Orders.Where(models.Users.Age.Gt(18))
