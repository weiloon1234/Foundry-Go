package invalid

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type other struct{}

func invalid() {
	_ = expr.Where(scalars.Records.Query(), expr.OrderNullableValue(expr.YearNullableValue(expr.Max(scalars.Records.Date).Value())).Gt(2024))
}
