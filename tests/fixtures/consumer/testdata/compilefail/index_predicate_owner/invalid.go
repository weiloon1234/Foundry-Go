package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.OnConflict(models.UserFields().Email).TargetWhere(models.WriteRecordFields().Name.Eq("schema"))
