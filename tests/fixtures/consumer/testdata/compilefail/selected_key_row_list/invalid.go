package invalid

import "foundry.test/consumer/models"

var _ = models.QueryUsers().DistinctOn(models.UserFields().Age.Value().Key())
