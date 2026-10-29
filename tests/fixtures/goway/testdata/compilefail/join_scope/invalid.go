package join_scope

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func F() {
	u := rows.As[struct{ U int }](models.Users.Query(), "u")
	o := rows.As[struct{ O int }](models.Orders.Query(), "o")
	rows.InnerJoin(u, o, rows.On(expr.At(rows.Scope(o), orm.ColumnExpr(models.Orders.BuyerID)), expr.At(rows.Scope(u), orm.KeyExpr(models.Users.ID))))
}
