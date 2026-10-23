package case_selected_predicate

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.WhenValue(expr.Count[models.User]().Gt(0), expr.Count[models.User]().Value()).Else(expr.Count[models.User]().Param(0).Value()).Eq(1)
