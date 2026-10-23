package phase_ordered_aggregate_to_row

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type owner struct{}

var _ = expr.OrderedRowExpression[owner, int64](expr.Count[owner]())
