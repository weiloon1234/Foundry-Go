package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type left struct{}
type right struct{}

var a = rows.As[left](models.Users.Query(), "a")
var b = rows.As[right](models.Orders.Query(), "b")
var j = rows.LeftJoin(a, b, rows.On(expr.At(rows.Scope(a), orm.KeyExpr(models.Users.ID)), expr.At(rows.Scope(b), orm.ColumnExpr(models.Orders.BuyerID))))
var invalid = rows.ForUpdate(rows.SelectRecord(j, rows.LeftScope(j, rows.Scope(a)))).Of(rows.Target(rows.NullableRightScope(j, rows.Scope(b))))
