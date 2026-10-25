package invalid

import "foundry.test/consumer/models"

var _ = models.Users.Email.SetNull()
