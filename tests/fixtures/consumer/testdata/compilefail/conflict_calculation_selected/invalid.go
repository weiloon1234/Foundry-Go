package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var fields = models.UserFieldsAt(models.QueryUsers().ConflictRows().Proposed())
var _ = query.SetConflictValue(models.UserFields().Age, fields.Age.Value())
