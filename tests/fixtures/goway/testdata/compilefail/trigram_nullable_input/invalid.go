package invalid

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.TrigramSimilarity[scalars.Record, string](expr.Value(orm.NullableTextExpr(scalars.Records.MaybeName)), "word")
