package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _, _ = query.SelectValue(models.QueryUsers(), query.Value(models.UserFields().Email)).ForUpdate().All(nil, (*database.DB)(nil))
