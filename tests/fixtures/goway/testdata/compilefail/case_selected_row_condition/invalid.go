package case_selected_row_condition

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.WhenValue(expr.OrderRow(expr.Value(models.Users.Age)).Gt(0), expr.Count[models.User]().Value())
