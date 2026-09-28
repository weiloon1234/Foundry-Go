package invalid

import "foundry.test/consumer/models"

var _ = models.QueryUsers().ForUpdate().Where(models.OrderFields().TotalCents.Gt(1))
