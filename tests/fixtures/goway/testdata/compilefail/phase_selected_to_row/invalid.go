package phase_selected_to_row

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type owner struct{}

var _ = expr.RowExpression[owner, int64](expr.Count[owner]().Value())
