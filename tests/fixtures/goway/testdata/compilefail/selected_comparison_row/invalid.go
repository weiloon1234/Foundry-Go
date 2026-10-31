package selected_comparison_row

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Where(models.Users.Query(), expr.GtValue(expr.OrderValue(expr.AsSelected(expr.Count[models.User]())), 2))
