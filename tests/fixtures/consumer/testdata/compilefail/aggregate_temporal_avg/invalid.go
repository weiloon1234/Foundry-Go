package invalid

import "foundry.test/consumer/models"

var invalid = models.UserFields().Birthday.Avg()
