package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	type a struct{}
	type b struct{}
	l := query.As[a](models.QueryUsers(), "l")
	r := query.As[b](models.QueryUsers(), "r")
	j := query.LeftJoin(l, r, query.On(models.UserFieldsAt(l.Scope()).ID, models.UserFieldsAt(r.Scope()).IntroducerID))
	c := query.Correlate(j, models.QueryOrders())
	_ = models.UserFieldsAt(query.OuterNullableScope(c, query.NullableRightScope(j, r.Scope())))
}
