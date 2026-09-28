package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type leftTag struct{}
type rightTag struct{}
type outsideTag struct{}

var a = query.As[leftTag](models.QueryUsers(), "a")
var b = query.As[rightTag](models.QueryUsers(), "b")
var af, bf = models.UserFieldsAt(a.Scope()), models.UserFieldsAt(b.Scope())
var j = query.LeftJoin(a, b, query.On(af.IntroducerID, bf.ID))
var outside = query.As[outsideTag](models.QueryUsers(), "outside")
var nullable = query.NullableRightScope(j, b.Scope())
var r = models.UserNullableFieldsAt(nullable)
var chain = query.LeftJoin(j, outside, query.On(r.ID, models.UserFieldsAt(outside.Scope()).ID))
var invalid = query.LeftScope(chain, nullable)
