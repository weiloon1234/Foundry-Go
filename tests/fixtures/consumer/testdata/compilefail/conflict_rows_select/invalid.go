package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var rows = models.QueryUsers().ConflictRows()
var fields = models.UserFieldsAt(rows.Proposed())
var _ = query.SelectValue(rows, fields.Age.Value())
