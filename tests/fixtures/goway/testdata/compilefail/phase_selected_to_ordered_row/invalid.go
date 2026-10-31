package phase_selected_to_ordered_row

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type owner struct{}

var _ = expr.OrderedRowExpression[owner, int64](expr.AsSelected(expr.Count[owner]()))
