package report_filter_or_phase

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Or(expr.ModelPredicate(orm.NumberGt(models.Users.Age, 1)), expr.Count[models.User]().Gt(0))
