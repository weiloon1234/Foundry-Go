package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var invalid = rows.ForUpdate(rows.ReadScalar(rows.SelectScalar(rows.From(models.Users.Query()), expr.Row(expr.Value(models.Users.Email)).Value()))).Of(rows.Target(rows.ModelScope(models.Orders.Query())))
