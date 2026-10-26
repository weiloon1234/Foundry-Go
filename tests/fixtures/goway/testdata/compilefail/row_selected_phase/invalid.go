package row_selected_phase

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type A struct{}

var _ = expr.Row(expr.Count[A]().Value())
