package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = expr.Lag(expr.Select(expr.Value(models.Orders.TotalCents)), 1, expr.WindowFor(rows.ModelScope(models.Users.Query())))
