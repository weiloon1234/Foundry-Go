package invalid

import "foundry.test/consumer/models"

var fields = models.UserFieldsAt(models.QueryUsers().ConflictRows().Proposed())
var _ = models.QueryUsers().Where(fields.Age.Gt(10))
