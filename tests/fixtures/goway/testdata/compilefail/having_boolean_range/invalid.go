package having_boolean_range

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.GtValue(expr.Exists[models.Order](), true)
