package join_full_scope

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
var on = rows.On(expr.At(rows.Scope(a), orm.NullableColumnExpr(models.Users.IntroducerID)), expr.At(rows.Scope(b), orm.KeyExpr(models.Users.ID)))
var j = rows.LeftJoin(a, b, on)
var full = rows.FullJoin(a, b, on)
var _ = rows.LeftScope(full, rows.Scope(a))
