package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var invalid = expr.Filter(expr.Over(expr.Count[models.Order](), expr.WindowFor(rows.ModelScope(models.Orders.Query()))), expr.ModelPredicate(orm.NumberGt(models.Orders.TotalCents, 0)))
