package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var base = models.UserFields()
var proposed = models.UserFieldsAt(models.QueryUsers().ConflictRows().Proposed())
var _ = query.SetConflictValue(query.Add(base.Age, base.Age), proposed.Age)
