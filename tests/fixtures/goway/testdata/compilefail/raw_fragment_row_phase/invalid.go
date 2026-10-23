package raw_fragment_row_phase

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Where(models.Users.Query(), expr.Raw[models.User, bool]("true"))
