package relation_expression_pivot_target

import (
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.RelationPivotOrder(j.Peers, expr.Negate(expr.Value(j.Targets.Rank)).Asc())
