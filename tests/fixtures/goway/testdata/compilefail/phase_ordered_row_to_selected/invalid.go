package phase_ordered_row_to_selected

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type owner struct{}

var _ = expr.Selected[owner, int64](expr.OrderedRowExpression[owner, int64]{})
