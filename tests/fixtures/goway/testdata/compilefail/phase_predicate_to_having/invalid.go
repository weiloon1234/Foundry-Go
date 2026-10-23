package phase_predicate_to_having

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type owner struct{}

var _ = expr.HavingPredicate[owner](expr.Predicate[owner]{})
