package case_phase_row_to_selected

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.ValueCase[models.User, string](expr.Case[models.User, string]{})
