package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var selected = rows.SelectScalar(rows.From(models.Users.Query()), expr.Count[models.User]().Value())
var invalid = rows.DistinctOnScalar(selected, expr.Group(expr.Value(models.Orders.ID)))
