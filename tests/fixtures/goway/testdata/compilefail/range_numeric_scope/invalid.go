package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = expr.NumericRange(expr.WindowFor(rows.ModelScope(models.Orders.Query())), expr.Row(expr.Value(models.Users.Age)).Value())
