package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type leftTag struct{}
type rightTag struct{}
type outsideTag struct{}

var a = rows.As[leftTag](models.Users.Query(), "a")
var b = rows.As[rightTag](models.Users.Query(), "b")
var af, bf = rows.Scope(a), rows.Scope(b)
var j = rows.LeftJoin(a, b, rows.On(expr.At(af, models.Users.IntroducerID), expr.At(bf, models.Users.ID)))
var outside = rows.As[outsideTag](models.Users.Query(), "outside")
var nullable = rows.NullableRightScope(j, rows.Scope(b))
var r = nullable
var chain = rows.LeftJoin(j, outside, rows.On(expr.AtNullable(r, models.Users.ID), expr.At(rows.Scope(outside), models.Users.ID)))
var invalid = rows.LeftScope(chain, nullable)
