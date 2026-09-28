package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = models.QueryOrders().OrderBy(query.Add(models.UserFields().Age, models.UserFields().Age).Asc())
