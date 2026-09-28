package invalid

import "foundry.test/consumer/models"

var invalid = models.UserRelations().Orders.Where(models.UserFields().Age.Gt(18))
