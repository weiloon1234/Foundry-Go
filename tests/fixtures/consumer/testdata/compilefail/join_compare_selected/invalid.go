package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

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
var _ = rows.OnEqual(expr.AsSelected(expr.Row(expr.At(af, orm.NumberExpr(models.Users.Age)))), expr.AsSelected(expr.Row(expr.At(bf, orm.NumberExpr(models.Users.Age)))))
