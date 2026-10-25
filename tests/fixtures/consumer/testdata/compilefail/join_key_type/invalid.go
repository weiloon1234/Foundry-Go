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
var o = rows.As[outsideTag](models.Orders.Query(), "o")
var invalid = rows.On(expr.At(af, models.Users.ID), expr.At(rows.Scope(o), models.Orders.ID))
