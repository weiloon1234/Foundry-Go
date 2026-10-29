package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var other = rows.ConflictRowsOf(models.WriteRecords.Query())
var _ = rows.ConflictWhereRows(orm.OnConflict(orm.ConflictKey(orm.TextExpr(models.Users.Email))).DoUpdate(orm.ConflictIncoming(orm.NumberExpr(models.Users.Age))), expr.At(other.Proposed(), orm.ColumnExpr(models.WriteRecords.Enabled)).Eq(true))
