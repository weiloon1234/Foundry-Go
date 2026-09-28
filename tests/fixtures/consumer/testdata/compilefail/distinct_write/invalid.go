package invalid

import "foundry.test/consumer/models"

var _ = models.QueryUsers().Distinct().Update
