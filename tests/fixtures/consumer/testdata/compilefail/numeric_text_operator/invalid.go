package invalid

import "foundry.test/consumer/models"

var invalid = models.UserFields().Age.Contains(42)
