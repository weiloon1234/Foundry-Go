package selected_boolean_order

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.OrderValue(expr.Exists[models.User]().Value())
