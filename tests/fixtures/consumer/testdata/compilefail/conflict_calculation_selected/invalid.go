package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var records = rows.ConflictRowsOf(models.Users.Query())
var _ = rows.SetConflictValue(orm.NumberExpr(models.Users.Age), expr.AsSelected(expr.Row(expr.At(records.Proposed(), orm.NumberExpr(models.Users.Age)))))
