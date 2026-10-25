package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.RelationFilter(models.UserRelations.Orders, expr.TextRow(models.Users.Email).Contains("x"))
