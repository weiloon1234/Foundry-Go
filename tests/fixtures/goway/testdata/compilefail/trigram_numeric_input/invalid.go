package invalid

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.TrigramMatch(expr.Value(scalars.Records.Latitude), 1.0)
