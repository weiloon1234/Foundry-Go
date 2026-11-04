package invalid

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ expr.OrderedRowExpression[scalars.Record, float64] = expr.TrigramSimilarityNullable(expr.Value(orm.NullableTextExpr(scalars.Records.MaybeName)), "word")
