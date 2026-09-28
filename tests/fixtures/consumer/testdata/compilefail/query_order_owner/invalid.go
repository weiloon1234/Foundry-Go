package invalid

import "foundry.test/consumer/models"

var invalid = models.QueryUsers().OrderBy(models.OrderFields().ID.Desc())
