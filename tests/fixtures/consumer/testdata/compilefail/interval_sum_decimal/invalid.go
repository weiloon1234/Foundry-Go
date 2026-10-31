package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
)

func invalid() {
	_ = expr.GtValue(expr.SumInterval(orm.IntervalExpr(intervalqueries.Samples.Period)), decimal.Decimal{})
}
