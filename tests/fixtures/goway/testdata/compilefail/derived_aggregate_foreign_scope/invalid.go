package derived_aggregate_foreign_scope

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.SelectScalar(rows.From(models.Users.Query()), expr.AsSelected(expr.SumRow(expr.RowExpression[models.Order, int]{})))
