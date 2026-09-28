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
var _ = query.OnEqual(af.Age.Value(), bf.Age.Value())
