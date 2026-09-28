package invalid

import "foundry.test/consumer/models"

var invalid = models.UserFields().Email.Sum()
