package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _, _ = query.SelectValue(models.QueryUsers(), models.UserFields().Email.Value()).ForUpdate().All(nil, (*database.DB)(nil))
