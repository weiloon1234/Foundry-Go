package case_selected_predicate

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Eq[models.User, int64](expr.WhenValue(expr.GtValue(expr.Count[models.User](), 0), expr.AsSelected(expr.Count[models.User]())).Else(expr.AsSelected(expr.ParamOf(expr.Count[models.User](), 0))), 1)
