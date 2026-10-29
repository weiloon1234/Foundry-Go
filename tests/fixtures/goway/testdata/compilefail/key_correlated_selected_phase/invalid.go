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
var scalar = rows.SelectCorrelatedScalar(c, expr.Row(child).Value())

var invalid = rows.DistinctOnCorrelatedScalarValues(scalar, expr.Group(child))
