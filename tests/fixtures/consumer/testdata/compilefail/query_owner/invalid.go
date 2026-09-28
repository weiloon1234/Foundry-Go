package invalid

import "foundry.test/consumer/models"

var invalid = models.QueryUsers().Where(models.OrderFields().TotalCents.Eq(10))
