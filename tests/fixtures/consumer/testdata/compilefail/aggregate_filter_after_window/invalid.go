package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var invalid = expr.Over(expr.Count[models.Order](), expr.WindowFor(rows.ModelScope(models.Orders.Query()))).Filter(expr.ModelPredicate(models.Orders.TotalCents.Gt(0)))
