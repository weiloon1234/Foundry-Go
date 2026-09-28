package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type left struct{}
type right struct{}

var a = query.As[left](models.QueryUsers(), "a")
var b = query.As[right](models.QueryOrders(), "b")
var j = query.LeftJoin(a, b, query.On(models.UserFieldsAt(a.Scope()).ID, models.OrderFieldsAt(b.Scope()).BuyerID))
var _ = query.UpdateLock[query.Left[query.Alias[left, models.User], query.Alias[right, models.Order]]](query.NullableRightScope(j, b.Scope()))
