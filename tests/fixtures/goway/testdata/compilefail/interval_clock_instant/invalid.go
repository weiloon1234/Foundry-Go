package invalid

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type other struct{}

func invalid() {
	_ = expr.InstantDifference(expr.Value(scalars.Records.Clock), expr.Value(scalars.Records.Instant))
}
