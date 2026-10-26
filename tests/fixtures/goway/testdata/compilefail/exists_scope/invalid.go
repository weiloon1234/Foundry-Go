package exists_scope

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type aTag struct{}
type bTag struct{}
type cTag struct{}

var a = rows.As[aTag](models.Users.Query(), "a")
var b = rows.As[bTag](models.Users.Query(), "b")
var c = rows.As[cTag](models.Users.Query(), "c")
var ageA = expr.At(rows.Scope(a), models.Users.Age)
var ageB = expr.At(rows.Scope(b), models.Users.Age)
var outer = rows.From(models.Users.Query())
var inner = rows.SelectScalar(outer, expr.Row(expr.Value(models.Users.ID)).Value())
var _ = expr.Where(models.Orders.Query(), rows.ExistsQuery(outer, rows.From(models.Orders.Query())))
