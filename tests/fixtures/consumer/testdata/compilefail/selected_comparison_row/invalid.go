package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = models.QueryUsers().Where(query.OrderValue(query.Count[models.User]().Value()).Gt(2))
