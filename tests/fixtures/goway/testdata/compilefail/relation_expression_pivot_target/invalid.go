package relation_expression_pivot_target

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.RelationPivotOrder(j.Peers, expr.Asc(expr.Negate(expr.Value(orm.NumberExpr(j.Targets.Rank)))))
