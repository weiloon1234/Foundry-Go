package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var parent = rows.From(models.Users.Query())
var child = rows.From(models.Orders.Query())
var c = rows.Correlate(parent, child)
var scope = rows.InnerScope(c, rows.ModelScope(models.Orders.Query()))
var record = rows.SelectCorrelatedRecord(c, scope)
var scalar = rows.SelectCorrelatedScalar(c, expr.Row(expr.At(scope, models.Orders.ID)).Value())
var invalid = rows.DistinctOnCorrelatedScalar(scalar, expr.Group(expr.Value(models.Users.ID)))
