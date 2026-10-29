package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type first struct{}
type second struct{}
type third struct{}

var a = rows.As[first](models.Users.Query(), "a")
var b = rows.As[second](models.Users.Query(), "b")
var c = rows.As[third](models.Users.Query(), "c")
var af, bf, cf = rows.Scope(a), rows.Scope(b), rows.Scope(c)
var invalid = rows.OnAnd(rows.On(expr.At(af, orm.KeyExpr(models.Users.ID)), expr.At(bf, orm.KeyExpr(models.Users.ID))), rows.On(expr.At(af, orm.KeyExpr(models.Users.ID)), expr.At(cf, orm.KeyExpr(models.Users.ID))))
