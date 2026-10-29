package case_aggregate_condition

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.When(expr.Count[models.User]().Gt(0), expr.Value(orm.TextExpr(models.Users.Email)))
