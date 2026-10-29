package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func invalid() {
	_ = expr.InstantDifference(expr.Value(orm.TimeExpr(intervalqueries.Samples.ClockStart)), expr.Value(orm.TimeExpr(intervalqueries.Samples.ClockEnd)))
}
