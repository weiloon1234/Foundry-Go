package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.SetQuery[models.Order](models.QueryUsers().Union(models.QueryUsers()))
