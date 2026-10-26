package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var invalid = query.SelectRecord(models.QueryUsers(), models.QueryOrders().Scope())
