package case_selected_row_condition

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.WhenValue(expr.Gt(expr.OrderRow(expr.Value(models.Users.Age)), 0), expr.AsSelected(expr.Count[models.User]()))
