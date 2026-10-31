package relation_expression_order_phase

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.RelationOrder(models.UserRelations.Orders, expr.DescValue(expr.Count[models.Order]()))
