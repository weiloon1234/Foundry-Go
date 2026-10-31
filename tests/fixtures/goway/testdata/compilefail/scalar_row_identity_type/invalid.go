package scalar_row_identity_type

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
var inner = rows.SelectScalar(outer, expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(models.Users.ID)))))
var orders = rows.SelectScalar(rows.From(models.Orders.Query()), expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(models.Orders.ID)))))
var _ = expr.Equal(expr.NullableRow(expr.Value(orm.KeyExpr(models.Users.ID))), rows.ScalarRowQuery(outer, orders))
