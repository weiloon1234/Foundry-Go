package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var selected = rows.SelectScalar(rows.From(models.Users.Query()), expr.AsSelected(expr.Count[models.User]()))
var invalid = rows.HavingScalar(selected, expr.GtValue(expr.Count[models.Order](), 0))
