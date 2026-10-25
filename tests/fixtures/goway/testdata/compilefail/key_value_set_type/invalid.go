package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var scalar = rows.Values(rows.SelectScalar(rows.From(models.Users.Query()), expr.Select(expr.Value(models.Users.Age))))
var combined = rows.UnionAllValues(scalar, scalar)

var otherInput = rows.Values(rows.SelectScalar(rows.From(models.Users.Query()), expr.Select(expr.Value(models.Users.Email))))
var other = rows.UnionAllValues(otherInput, otherInput)
var invalid = rows.DistinctOnValueSet(combined, expr.Select(other.Value()).Key())
