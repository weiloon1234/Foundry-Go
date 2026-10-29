package subquery_scalar_source

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

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
var ageA = expr.At(rows.Scope(a), orm.NumberExpr(models.Users.Age))
var ageB = expr.At(rows.Scope(b), orm.NumberExpr(models.Users.Age))
var outer = rows.From(models.Users.Query())
var inner = rows.SelectScalar(outer, expr.Row(expr.Value(orm.KeyExpr(models.Users.ID))).Value())
var _ = rows.InQuery(expr.Value(orm.KeyExpr(models.Users.ID)), rows.FromRows(rows.SelectValue(models.Users.Query(), orm.KeyExpr(models.Users.ID))))
