package invalid

import "foundry.test/consumer/models"

var _ = models.OrderFields().TotalCents.Sum().Filter(models.OrderFields().TotalCents.Gt("1"))
