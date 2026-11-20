package key_phase_row_to_selected

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.ProjectionKey[models.User](expr.Group(expr.Value(models.Users.Age)))
