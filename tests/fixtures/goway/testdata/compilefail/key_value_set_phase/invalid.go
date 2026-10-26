package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var scalar = rows.Values(rows.SelectScalar(rows.From(models.Users.Query()), expr.Row(expr.Value(models.Users.Age)).Value()))
var combined = rows.UnionAllValues(scalar, scalar)

var invalid = rows.DistinctOnValueSet(combined, expr.Group(combined.Value()))
