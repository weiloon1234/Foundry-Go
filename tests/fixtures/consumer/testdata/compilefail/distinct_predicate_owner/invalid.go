package invalid

import "foundry.test/consumer/models"

var _ = models.QueryUsers().Distinct().Where(models.OrderFields().TotalCents.Eq(3))
