package report_filter_or_phase

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Or(expr.ModelPredicate(models.Users.Age.Gt(1)), expr.Count[models.User]().Gt(0))
