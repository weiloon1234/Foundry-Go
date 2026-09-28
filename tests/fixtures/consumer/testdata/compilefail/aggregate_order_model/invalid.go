package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var invalid = models.QueryOrders().OrderBy(query.Count[models.Order]().Desc())
