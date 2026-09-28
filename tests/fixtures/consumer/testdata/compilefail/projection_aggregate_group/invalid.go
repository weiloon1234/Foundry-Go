package invalid

import "foundry.test/consumer/models"

var invalid = models.OrderFields().TotalCents.Sum().Value().Group()
