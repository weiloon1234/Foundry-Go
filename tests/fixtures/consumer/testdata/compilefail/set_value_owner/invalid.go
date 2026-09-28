package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var a = query.SelectValue(models.QueryUsers(), models.UserFields().ID.Value())
var b = query.SelectValue(models.QueryOrders(), models.OrderFields().ID.Value())
var _ = a.Union(b)
