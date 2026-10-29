package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.OnConflict(orm.ConflictKey(orm.TextExpr(models.Users.Email))).TargetWhere(orm.TextEq(models.WriteRecords.Name, "schema"))
