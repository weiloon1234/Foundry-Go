package invalid

import (
	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func invalid() {
	_ = expr.AddIntervals(expr.Value(intervalqueries.Samples.Period), expr.Value(intervalqueries.Samples.MaybePeriod))
}
