package invalid

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var needle int64
var _ = expr.TrigramSimilarity(expr.Value(scalars.Records.Name), needle)
