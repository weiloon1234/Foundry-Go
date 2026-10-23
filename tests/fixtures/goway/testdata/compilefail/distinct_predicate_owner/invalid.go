package distinct_predicate_owner

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.Distinct(rows.FromModel(models.Users.Query())).Where(expr.Value(models.Orders.TotalCents).Eq(3))
