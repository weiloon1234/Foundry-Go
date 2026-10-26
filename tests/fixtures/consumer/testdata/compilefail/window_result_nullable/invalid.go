package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/windowqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.MapSelected(windowqueries.Rankings.Number, expr.Lag(expr.Row(expr.Value(models.Orders.TotalCents)).Value(), 1, expr.WindowFor(rows.ModelScope(models.Orders.Query()))))
