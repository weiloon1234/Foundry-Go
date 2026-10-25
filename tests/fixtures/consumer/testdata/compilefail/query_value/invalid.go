package invalid

import "foundry.test/consumer/models"

var invalid = models.Users.Email.Eq(42)
