package invalid

import "foundry.test/consumer/models"

var invalid = models.UserRelations().Groups.OrderByPivot(models.GroupFields().Name.Asc())
