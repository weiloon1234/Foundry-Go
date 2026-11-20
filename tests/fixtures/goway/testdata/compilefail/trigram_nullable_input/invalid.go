package invalid

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.TrigramSimilarity[scalars.Record, string](expr.Value(scalars.Records.MaybeName), "word")
