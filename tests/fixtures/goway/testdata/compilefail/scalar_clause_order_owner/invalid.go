package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var selected = rows.SelectScalar(rows.From(models.Users.Query()), expr.Row(expr.Value(orm.NumberExpr(models.Users.Age))).Value())
var invalid = rows.OrderScalar(selected, expr.Value(orm.NumberExpr(models.Orders.TotalCents)).Asc())
