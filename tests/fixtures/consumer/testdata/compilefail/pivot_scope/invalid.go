package invalid

import "foundry.test/consumer/models"

var invalid = models.UserRelations().Groups.WherePivot(models.GroupFields().Name.Eq("member"))
