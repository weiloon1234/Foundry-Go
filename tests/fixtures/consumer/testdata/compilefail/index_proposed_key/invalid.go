package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var records = rows.ConflictRowsOf(models.Users.Query())
var _ = rows.OnConflictKeys[models.User](expr.Group(expr.At(records.Proposed(), models.Users.Email)))
