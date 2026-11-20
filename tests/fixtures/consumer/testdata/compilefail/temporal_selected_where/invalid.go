package invalid

import (
	"foundry.test/consumer/temporalqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func invalid() {
	f := temporalqueries.Samples
	_ = expr.Where(temporalqueries.Samples.Query(), expr.GtValue(expr.OrderNullableValue(expr.YearNullableValue(expr.AsSelected(expr.Max(f.Date)))), 2024))
}
