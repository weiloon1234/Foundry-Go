package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.ScalarQuery[models.User, models.User](models.QueryUsers(), models.QueryUsers().Union(models.QueryUsers()))
