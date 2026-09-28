package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type aTag struct{}
type bTag struct{}

var a = query.As[aTag](models.QueryUsers(), "a")
var b = query.As[bTag](models.QueryUsers(), "b")
var af = models.UserFieldsAt(a.Scope())
var bf = models.UserFieldsAt(b.Scope())

type cTag struct{}

var left = query.LeftJoin(a, b, query.On(af.ID, bf.ID))
var crossed = query.CrossJoin(left, query.As[cTag](models.QueryUsers(), "c"))
var _ = query.LeftScope(crossed, query.NullableRightScope(left, b.Scope()))
