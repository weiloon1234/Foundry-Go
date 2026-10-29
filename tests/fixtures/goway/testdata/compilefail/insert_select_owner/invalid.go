package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.InsertFrom(rows.From(models.WriteRecords.Query()), models.WriteRecords.Query()).Select(rows.MapInsert(orm.TextExpr(models.WriteRecords.Name), expr.Value(orm.TextExpr(models.Users.Email))))
