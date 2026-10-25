package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.OnConflict(orm.ConflictKey(models.Users.Email)).TargetWhere(models.WriteRecords.Name.Eq("schema"))
