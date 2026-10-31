package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = models.Users.Query().Where(expr.RelationFilter(models.UserRelations.Orders, expr.Gt(expr.OrderRow(expr.Value(orm.NumberExpr(models.Orders.TotalCents))), 0)))
