package key_phase_selected_to_row

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.GroupKey[models.User](expr.Row(expr.Value(models.Users.Age)).Value().Key())
