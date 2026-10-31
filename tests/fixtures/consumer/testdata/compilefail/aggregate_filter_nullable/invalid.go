package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

var invalid = expr.GtValue(expr.Filter(expr.Sum(orm.NumberExpr(models.Orders.TotalCents)), expr.ModelPredicate(orm.NumberGt(models.Orders.TotalCents, 0))), value.Of(decimal.FromInt64(1)))
