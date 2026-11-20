package join_scope

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func F() {
	u := rows.As[struct{ U int }](models.Users.Query(), "u")
	o := rows.As[struct{ O int }](models.Orders.Query(), "o")
	rows.InnerJoin(u, o, rows.On(expr.At(rows.Scope(o), models.Orders.BuyerID), expr.At(rows.Scope(u), models.Users.ID)))
}
