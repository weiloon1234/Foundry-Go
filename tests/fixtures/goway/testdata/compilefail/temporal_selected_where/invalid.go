package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type other struct{}

func invalid() {
	_ = expr.Where(scalars.Records.Query(), expr.GtValue(expr.OrderNullableValue(expr.YearNullableValue(expr.AsSelected(expr.Max(orm.TimeExpr(scalars.Records.Date))))), 2024))
}
