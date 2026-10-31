package interval_result_phase

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type sample struct {
	ID     int64
	Period temporal.Interval
}

var period orm.Interval[sample]
var _ = expr.Where((orm.Table[sample, int64]{}).Query(), expr.GtValue(expr.SumInterval(orm.IntervalExpr(period)), temporal.Days(1)))
