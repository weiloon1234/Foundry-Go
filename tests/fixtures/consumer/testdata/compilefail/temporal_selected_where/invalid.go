package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/temporalqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func invalid() {
	f := temporalqueries.Samples
	_ = expr.Where(temporalqueries.Samples.Query(), expr.GtValue(expr.OrderNullableValue(expr.YearNullableValue(expr.AsSelected(expr.Max(orm.TimeExpr(f.Date))))), 2024))
}
