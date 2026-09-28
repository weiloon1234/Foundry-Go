package invalid

import "foundry.test/consumer/models"

var invalid = models.QueryOrders().Where(models.UserRelations().Orders.Exists())
