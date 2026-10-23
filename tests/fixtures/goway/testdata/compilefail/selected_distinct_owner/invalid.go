package selected_distinct_owner

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.DistinctOnValues(rows.FromModel(models.Users.Query()), expr.Select(expr.Value(models.Orders.TotalCents)).Key())
