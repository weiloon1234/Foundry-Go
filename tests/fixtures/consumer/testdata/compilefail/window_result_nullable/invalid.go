package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/windowqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.MapSelected(windowqueries.Rankings.Number, expr.Lag(expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(models.Orders.TotalCents)))), 1, expr.WindowFor(rows.ModelScope(models.Orders.Query()))))
