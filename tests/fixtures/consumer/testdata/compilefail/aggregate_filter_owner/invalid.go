package invalid

import "foundry.test/consumer/models"

var _ = models.OrderFields().TotalCents.Sum().Filter(models.UserFields().Age.Gt(18))
