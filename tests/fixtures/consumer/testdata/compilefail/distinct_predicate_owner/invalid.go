package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.WhereScalar(rows.DistinctScalar(rows.SelectScalar(rows.From(models.Users.Query()), expr.Select(expr.Value(models.Users.Age)))), expr.Value(models.Orders.TotalCents).Eq(3))
