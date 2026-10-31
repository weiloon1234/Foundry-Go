package phase_having_to_predicate

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type owner struct{}

var _ = expr.Predicate[owner](expr.GtValue(expr.Count[owner](), 0))
