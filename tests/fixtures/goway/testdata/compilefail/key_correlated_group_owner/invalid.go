package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var c = rows.Correlate(rows.From(models.Users.Query()), rows.From(models.Orders.Query()))
var scope = rows.InnerScope(c, rows.ModelScope(models.Orders.Query()))
var child = expr.At(scope, orm.NumberExpr(models.Orders.TotalCents))
var scalar = rows.SelectCorrelatedScalar(c, expr.AsSelected(expr.Row(child)))

var invalid = rows.GroupByCorrelatedScalar(scalar, expr.Group(expr.Value(orm.NumberExpr(models.Orders.TotalCents))))
