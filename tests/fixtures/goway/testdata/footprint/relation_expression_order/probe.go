package relation_expression_order

import (
	"foundry.test/goway/models"
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func Direct() orm.Query[models.User] {
	r := models.UserRelations.Orders
	return models.Users.Query().With(r.OrderBy(expr.RelationOrder(r, expr.Asc(expr.Negate(expr.Value(orm.NumberExpr(models.Orders.TotalCents)))))))
}
func Joined() orm.Query[j.Parent] {
	r := j.Peers
	return j.Parents.Query().With(r.OrderBy(expr.RelationOrder(r, expr.Asc(expr.Negate(expr.Value(orm.NumberExpr(j.Targets.Rank)))))).OrderByPivot(expr.RelationPivotOrder(r, expr.Asc(expr.Lower(expr.Value(orm.TextExpr(j.Pivots.Role)))))))
}
