package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

var invalid = expr.Sum(orm.NumberExpr(models.Orders.TotalCents)).Filter(expr.ModelPredicate(orm.NumberGt(models.Orders.TotalCents, 0))).Gt(value.Of(decimal.FromInt64(1)))
