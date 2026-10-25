package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type left struct{}
type right struct{}

var a = rows.As[left](models.Users.Query(), "a")
var b = rows.As[right](models.Orders.Query(), "b")
var j = rows.LeftJoin(a, b, rows.On(expr.At(rows.Scope(a), models.Users.ID), expr.At(rows.Scope(b), models.Orders.BuyerID)))
var invalid = rows.UpdateLock[rows.Left[rows.Alias[left, models.User], rows.Alias[right, models.Order]]](rows.Target(rows.NullableRightScope(j, rows.Scope(b))))
