package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var other = rows.ConflictRowsOf(models.WriteRecords.Query())
var _ = rows.ConflictWhereRows(orm.OnConflict(orm.ConflictKey(models.Users.Email)).DoUpdate(orm.ConflictIncoming(models.Users.Age)), expr.At(other.Proposed(), models.WriteRecords.Enabled).Eq(true))
