package invalid

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.TrigramMatch[scalars.Record, string](expr.AsSelected(expr.Row(expr.Value(orm.TextExpr(scalars.Records.Name)))), "word")
