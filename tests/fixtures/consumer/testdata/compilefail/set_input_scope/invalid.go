package invalid

import "foundry.test/consumer/models"

var combined = models.QueryUsers().Union(models.QueryUsers())
var _ = combined.Where(models.UserFields().Age.Gt(20))
