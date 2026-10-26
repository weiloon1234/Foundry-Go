package invalid

import "foundry.test/consumer/models"

var combined = models.QueryUsers().Union(models.QueryUsers())
var f = models.UserFieldsAt(combined.Scope())
var _ = models.QueryUsers().Where(f.Age.Gt(20))
