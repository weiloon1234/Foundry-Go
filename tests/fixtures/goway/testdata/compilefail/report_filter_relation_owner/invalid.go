package report_filter_relation_owner

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Has(models.UserRelations.Orders, expr.ModelPredicate(models.Users.Age.Gt(1)))
