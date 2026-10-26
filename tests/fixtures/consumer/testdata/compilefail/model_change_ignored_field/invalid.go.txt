package invalid

import "foundry.test/consumer/models"

var _ = (models.UserChanges{}).Fields().Scratch
