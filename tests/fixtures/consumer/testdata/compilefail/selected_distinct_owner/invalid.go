package invalid

import "foundry.test/consumer/models"

var _ = models.QueryUsers().DistinctOnValues(models.OrderFields().TotalCents.Value().Key())
