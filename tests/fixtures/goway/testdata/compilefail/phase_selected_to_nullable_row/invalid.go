package phase_selected_to_nullable_row

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type owner struct{}

var _ = expr.NullableOrderedRowExpression[owner, int64](expr.Nullable(expr.AsSelected(expr.Count[owner]())))
