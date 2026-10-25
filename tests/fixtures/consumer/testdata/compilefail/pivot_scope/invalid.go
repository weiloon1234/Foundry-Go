package invalid

import "foundry.test/consumer/models"

var invalid = models.UserLinks.Groups.WherePivot(models.Groups.Name.Eq("member"))
