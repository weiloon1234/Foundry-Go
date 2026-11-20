package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type aTag struct{}
type bTag struct{}

var a = rows.As[aTag](models.Users.Query(), "a")
var b = rows.As[bTag](models.Users.Query(), "b")
var af = rows.Scope(a)
var bf = rows.Scope(b)
var _ = rows.InnerJoin(a, b, rows.OnLess(expr.At(bf, models.Users.Age), expr.At(af, models.Users.Age)))
