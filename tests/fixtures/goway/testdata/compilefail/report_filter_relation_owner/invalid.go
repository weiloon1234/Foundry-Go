package report_filter_relation_owner

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Has(models.UserRelations.Orders, expr.ModelPredicate(orm.NumberGt(models.Users.Age, 1)))
