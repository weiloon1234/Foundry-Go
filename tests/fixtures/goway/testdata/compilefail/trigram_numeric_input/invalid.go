package invalid

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.TrigramMatch(expr.Value(orm.NumberExpr(scalars.Records.Latitude)), 1.0)
