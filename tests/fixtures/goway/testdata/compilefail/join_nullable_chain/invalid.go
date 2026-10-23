package join_nullable_chain

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
var on = rows.On(expr.At(rows.Scope(a), models.Users.IntroducerID), expr.At(rows.Scope(b), models.Users.ID))
var j = rows.LeftJoin(a, b, on)
var nullable = rows.NullableRightScope(j, rows.Scope(b))
var chain = rows.LeftJoin(j, c, rows.On(expr.AtNullable(nullable, models.Users.ID), expr.At(rows.Scope(c), models.Users.ID)))
var _ = rows.LeftScope(chain, nullable)
