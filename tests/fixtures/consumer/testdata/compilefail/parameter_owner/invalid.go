package invalid

import (
	"foundry.test/consumer/models"
	foundryquery "github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = models.QueryOrders().Where(foundryquery.Param(models.UserFields().Age, 2).Eq(2))
