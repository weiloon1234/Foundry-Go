package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var invalid = expr.Filter(expr.Sum(orm.NumberExpr(models.Orders.TotalCents)), expr.ModelPredicate(orm.NumberGt(models.Orders.TotalCents, "1")))
