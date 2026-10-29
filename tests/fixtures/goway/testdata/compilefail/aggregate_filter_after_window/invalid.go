package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = expr.Over(expr.Count[models.Order](), expr.WindowFor(rows.ModelScope(models.Orders.Query()))).Filter(expr.Value(orm.NumberExpr(models.Orders.TotalCents)).Eq(0))
