package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.DistinctOnScalar(rows.SelectScalar(rows.From(models.Users.Query()), expr.Row(expr.Value(models.Users.Age)).Value()), expr.Group(expr.Value(models.Orders.ID)))
