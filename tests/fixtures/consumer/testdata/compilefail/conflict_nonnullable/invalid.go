package invalid

import "foundry.test/consumer/models"

var _ = models.UserFields().Email.SetNull()
