package relation_expression_order_target

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.RelationOrder(models.UserRelations.Orders, expr.Asc(expr.Negate(expr.Value(orm.NumberExpr(models.Users.Age)))))
