package phase_nullable_row_to_selected

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/value"
)

type owner struct{}

var _ = expr.Selected[owner, value.Nullable[int64]](expr.NullableOrderedRowExpression[owner, int64]{})
