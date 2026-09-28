package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type first struct{}
type second struct{}
type third struct{}

var a = query.As[first](models.QueryUsers(), "a")
var b = query.As[second](models.QueryUsers(), "b")
var c = query.As[third](models.QueryUsers(), "c")
var af, bf, cf = models.UserFieldsAt(a.Scope()), models.UserFieldsAt(b.Scope()), models.UserFieldsAt(c.Scope())
var invalid = query.OnAnd(query.On(af.ID, bf.ID), query.On(af.ID, cf.ID))
