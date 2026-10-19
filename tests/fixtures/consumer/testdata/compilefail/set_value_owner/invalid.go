package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var a = query.SelectValue(models.QueryUsers(), query.Value(models.UserFields().ID))
var b = query.SelectValue(models.QueryOrders(), query.Value(models.OrderFields().ID))
var _ = a.Union(b)
