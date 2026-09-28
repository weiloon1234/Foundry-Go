package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var invalid = models.QueryOrders().Where(query.Count[models.Order]().Gt(1))
