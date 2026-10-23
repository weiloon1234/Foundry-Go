package raw_fragment_group_phase

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Group(expr.Raw[models.User, int64]("1"))
