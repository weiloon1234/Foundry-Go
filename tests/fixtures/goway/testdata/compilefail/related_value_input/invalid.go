package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.RelatedValue(models.UserRelations.Orders, expr.Sum(orm.NumberExpr(models.Users.Age)))
