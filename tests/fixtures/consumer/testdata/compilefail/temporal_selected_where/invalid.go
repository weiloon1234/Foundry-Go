package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/temporalqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func invalid() {
	f := temporalqueries.Samples
	_ = expr.Where(temporalqueries.Samples.Query(), expr.OrderNullableValue(expr.YearNullableValue(expr.Max(orm.TimeExpr(f.Date)).Value())).Gt(2024))
}
