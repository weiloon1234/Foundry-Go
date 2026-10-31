package interval_sum_decimal

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type sample struct {
	ID     int64
	Period temporal.Interval
}

var period orm.Interval[sample]
var _ = expr.GtValue(expr.SumInterval(orm.IntervalExpr(period)), decimal.Decimal{})
