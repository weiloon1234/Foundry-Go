package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var records = rows.ConflictRowsOf(models.Users.Query())
var _ = rows.SetConflictValue(expr.Add(expr.Value(models.Users.Age), expr.Value(models.Users.Age)), expr.At(records.Proposed(), models.Users.Age))
