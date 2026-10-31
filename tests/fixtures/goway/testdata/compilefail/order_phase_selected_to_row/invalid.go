package order_phase_selected_to_row

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Order[models.User](expr.DescValue(expr.Count[models.User]()))
