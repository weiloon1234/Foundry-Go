package case_phase_selected_to_row

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Case[models.User, string](expr.ValueCase[models.User, string]{})
