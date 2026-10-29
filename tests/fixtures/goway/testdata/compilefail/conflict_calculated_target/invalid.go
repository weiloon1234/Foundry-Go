package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var records = rows.ConflictRowsOf(models.Users.Query())
var _ = rows.SetConflictValue(expr.Add(expr.Value(orm.NumberExpr(models.Users.Age)), expr.Value(orm.NumberExpr(models.Users.Age))), expr.At(records.Proposed(), orm.NumberExpr(models.Users.Age)))
