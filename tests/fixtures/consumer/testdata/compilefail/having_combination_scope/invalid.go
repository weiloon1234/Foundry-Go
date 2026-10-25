package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var invalid = expr.HavingAnd(expr.Count[models.User]().Gt(1), expr.Count[models.Order]().Gt(1))
