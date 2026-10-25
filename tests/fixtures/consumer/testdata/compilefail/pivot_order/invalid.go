package invalid

import "foundry.test/consumer/models"

var invalid = models.UserLinks.Groups.OrderByPivot(models.Groups.Name.Asc())
