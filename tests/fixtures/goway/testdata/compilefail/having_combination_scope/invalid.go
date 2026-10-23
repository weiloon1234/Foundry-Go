package having_combination_scope

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.HavingAnd(expr.Count[models.User]().Gt(1), expr.Count[models.Order]().Gt(1))
