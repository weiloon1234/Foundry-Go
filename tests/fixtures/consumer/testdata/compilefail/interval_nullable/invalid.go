package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func invalid() {
	_ = expr.AddIntervals(expr.Value(orm.IntervalExpr(intervalqueries.Samples.Period)), expr.Value(orm.NullableIntervalExpr(intervalqueries.Samples.MaybePeriod)))
}
