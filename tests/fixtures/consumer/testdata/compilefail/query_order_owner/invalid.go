package invalid

import "foundry.test/consumer/models"

var invalid = models.Users.Query().OrderBy(models.Orders.ID.Desc())
