package lateralqueries

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

// doc-example:begin lateral-joins
type OrdersAlias struct{}

func CorrelatedOrders[L any](outer rows.ScopeSource[L], scope expr.Scope[L, models.User]) (rows.CorrelatedSource[L, rows.Alias[OrdersAlias, models.Order]], expr.Scope[rows.Correlation[L, rows.Alias[OrdersAlias, models.Order]], models.Order]) {
	orders := rows.As[OrdersAlias](models.Orders.Query(), "orders_for_user")
	c := rows.Correlate(outer, orders)
	inner := rows.InnerScope(c, rows.Scope(orders))
	u, o := rows.OuterScope(c, scope), inner
	return c.Where(expr.Equal(expr.At(o, models.Orders.BuyerID), expr.At(u, models.Users.ID))), inner
}

// doc-example:end lateral-joins
