package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var selected = rows.SelectScalar(rows.From(models.Users.Query()), expr.AsSelected(expr.Row(expr.Value(models.Users.Age))))
var invalid = rows.OrderScalar(selected, expr.Asc(expr.Value(models.Orders.TotalCents)))
