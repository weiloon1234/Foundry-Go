package invalid

import (
	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func invalid() {
	_ = expr.InstantDifference(expr.Value(intervalqueries.Samples.ClockStart), expr.Value(intervalqueries.Samples.ClockEnd))
}
