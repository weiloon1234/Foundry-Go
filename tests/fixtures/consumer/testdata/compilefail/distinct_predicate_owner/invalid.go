package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.WhereScalar(rows.DistinctScalar(rows.SelectScalar(rows.From(models.Users.Query()), expr.AsSelected(expr.Row(expr.Value(models.Users.Age))))), expr.Eq(expr.Value(models.Orders.TotalCents), 3))
