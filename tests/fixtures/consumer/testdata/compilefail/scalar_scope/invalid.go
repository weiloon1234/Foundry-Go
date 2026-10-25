package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var inner = rows.SelectScalar(rows.From(models.Users.Query()), expr.Select(expr.Value(models.Users.ID)))
var scalar = rows.ScalarRowQuery(rows.From(models.Orders.Query()), inner)
var invalid = expr.Equal(expr.NullableRow(expr.Value(models.Users.ID)), scalar)
