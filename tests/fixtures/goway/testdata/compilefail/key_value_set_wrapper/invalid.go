package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var scalar = rows.Values(rows.SelectScalar(rows.From(models.Users.Query()), expr.Select(expr.Value(models.Users.Age))))
var combined = rows.UnionAllValues(scalar, scalar)

type forged struct{ rows.ValueSetQuery[int] }

var invalid = rows.DistinctOnValueSet(forged{combined}, expr.Select(combined.Value()).Key())
