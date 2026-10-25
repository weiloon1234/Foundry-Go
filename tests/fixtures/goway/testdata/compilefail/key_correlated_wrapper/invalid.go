package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var c = rows.Correlate(rows.From(models.Users.Query()), rows.From(models.Orders.Query()))
var scope = rows.InnerScope(c, rows.ModelScope(models.Orders.Query()))
var child = expr.At(scope, models.Orders.TotalCents)
var scalar = rows.SelectCorrelatedScalar(c, expr.Select(child))

type forged struct {
	rows.CorrelatedScalar[models.User, models.Order, int64]
}

var invalid = rows.GroupByCorrelatedScalar(forged{scalar}, expr.Group(child))
