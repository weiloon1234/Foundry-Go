package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var invalid = expr.Sum(models.Orders.TotalCents).Filter(expr.ModelPredicate(models.Orders.TotalCents.Gt("1")))
