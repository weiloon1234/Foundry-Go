package invalid

import "foundry.test/consumer/models"

var _ = models.QueryUsers().Union(models.QueryOrders())
