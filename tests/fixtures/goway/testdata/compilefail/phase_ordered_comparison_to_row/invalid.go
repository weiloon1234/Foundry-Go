package phase_ordered_comparison_to_row

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type owner struct{}

var _ = expr.OrderedRowExpression[owner, int64](expr.OrderValue(expr.AsSelected(expr.Count[owner]())))
