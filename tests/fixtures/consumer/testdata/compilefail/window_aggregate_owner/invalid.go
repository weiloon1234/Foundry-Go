package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.Count[models.User]().Over(query.WindowFor(models.QueryOrders()))
