package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.Conflict[models.Order](query.OnConflict(models.UserFields().Email).DoNothing())
