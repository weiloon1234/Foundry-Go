package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type parent struct{}
type child struct{}
type named struct{}
type other struct{}

var a = query.As[parent](models.QueryUsers(), "a")
var b = query.As[child](models.QueryOrders(), "b")
var c = query.Correlate(a, b)
var scope = query.InnerScope(c, b.Scope())
var selected = query.SelectCorrelatedRecord(c, scope)
var lateral = query.AsLateral[named](selected, "lateral")
var on = query.On(models.OrderFieldsAt(lateral.Scope()).BuyerID, models.UserFieldsAt(a.Scope()).ID)
var _ = query.LeftJoinLateral(a, lateral, on)
