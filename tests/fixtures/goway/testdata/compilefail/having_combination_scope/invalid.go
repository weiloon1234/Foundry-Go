package having_combination_scope

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.HavingAnd(expr.GtValue(expr.Count[models.User](), 1), expr.GtValue(expr.Count[models.Order](), 1))
