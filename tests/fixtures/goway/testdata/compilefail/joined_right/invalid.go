package joined_right

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func F() {
	u := rows.As[struct{ U int }](models.Users.Query(), "u")
	o := rows.As[struct{ O int }](models.Orders.Query(), "o")
	j := rows.InnerJoin(u, o, rows.On(expr.At(rows.Scope(u), models.Users.ID), expr.At(rows.Scope(o), models.Orders.BuyerID)))
	right := expr.At(rows.LeftScope(j, rows.Scope(u)), models.Users.ID)
	rows.InnerJoin(u, j, rows.On(expr.At(rows.Scope(u), models.Users.ID), right))
}
