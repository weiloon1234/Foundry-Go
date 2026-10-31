package selected_calculation_order

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var c = expr.Count[models.User]()
var _ = expr.OrderBy(models.Users.Query(), expr.AscValue(expr.AddValue(expr.AsSelected(c), expr.AsSelected(c))))
