package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func invalid() {
	_ = expr.NegateInterval(expr.AsSelected(expr.SumInterval(orm.IntervalExpr(intervalqueries.Samples.Period))))
}
