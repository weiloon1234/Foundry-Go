package invalid

import "foundry.test/consumer/models"

var invalid = models.Users.Age.Contains(42)
