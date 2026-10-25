package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

var invalid = expr.Sum(models.Orders.TotalCents).Filter(expr.ModelPredicate(models.Orders.TotalCents.Gt(0))).Gt(value.Of(decimal.FromInt64(1)))
