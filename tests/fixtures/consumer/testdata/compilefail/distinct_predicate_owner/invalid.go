package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.WhereScalar(rows.DistinctScalar(rows.SelectScalar(rows.From(models.Users.Query()), expr.Row(expr.Value(orm.NumberExpr(models.Users.Age))).Value())), expr.Value(orm.NumberExpr(models.Orders.TotalCents)).Eq(3))
