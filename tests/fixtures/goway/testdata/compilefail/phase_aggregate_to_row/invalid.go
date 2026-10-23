package phase_aggregate_to_row

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type owner struct{}

var _ = expr.RowExpression[owner, bool](expr.Exists[owner]())
