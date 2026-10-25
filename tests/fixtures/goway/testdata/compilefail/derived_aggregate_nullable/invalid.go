package derived_aggregate_nullable

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ = expr.SumRow(expr.RowExpression[models.User, value.Nullable[int]]{})
