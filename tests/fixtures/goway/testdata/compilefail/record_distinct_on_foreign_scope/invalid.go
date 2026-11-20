package record_distinct_on_foreign_scope

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.DistinctOnRecord(rows.SelectRecord(rows.From(models.Users.Query()), rows.ModelScope(models.Users.Query())), expr.Group(expr.Value(models.Orders.TotalCents)))
