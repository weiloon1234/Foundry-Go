package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type leftTag struct{}
type rightTag struct{}

var a = query.As[leftTag](models.QueryUsers(), "a")
var b = query.As[rightTag](models.QueryUsers(), "b")
var joined = query.InnerJoin(a, b, query.On(models.UserFieldsAt(a.Scope()).IntroducerID, models.UserFieldsAt(b.Scope()).ID))
var invalid = query.SelectRecord(joined, a.Scope())
