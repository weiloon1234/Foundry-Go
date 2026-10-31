package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var invalid = expr.HavingAnd(expr.GtValue(expr.Count[models.User](), 1), expr.GtValue(expr.Count[models.Order](), 1))
