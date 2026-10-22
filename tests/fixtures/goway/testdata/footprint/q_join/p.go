package q_join

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type userSide struct{}
type orderSide struct{}

func F() rows.InnerJoined[rows.Alias[userSide, models.User], rows.Alias[orderSide, models.Order]] {
	u := rows.As[userSide](models.Users.Query(), "users")
	o := rows.As[orderSide](models.Orders.Query(), "orders")
	return rows.InnerJoin(u, o, rows.On(expr.At(rows.Scope(u), models.Users.ID), expr.At(rows.Scope(o), models.Orders.BuyerID)))
}
