package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.RelationFilter(models.UserRelations.Orders, expr.TextContains(expr.TextRow(orm.TextExpr(models.Users.Email)), "x"))
