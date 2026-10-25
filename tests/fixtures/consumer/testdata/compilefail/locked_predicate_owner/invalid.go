package invalid

import "foundry.test/consumer/models"

var _ = models.Users.Query().ForUpdate().Where(models.Orders.TotalCents.Gt(1))
