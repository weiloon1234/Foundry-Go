package case_aggregate_row_value

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.When(expr.OrderRow(expr.Value(models.Users.Age)).Gt(0), expr.Count[models.User]().Value())
