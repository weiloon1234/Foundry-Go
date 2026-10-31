package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var scalar = rows.Values(rows.SelectScalar(rows.From(models.Users.Query()), expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(models.Users.Age))))))
var combined = rows.UnionAllValues(scalar, scalar)

var otherInput = rows.Values(rows.SelectScalar(rows.From(models.Users.Query()), expr.AsSelected(expr.Row(expr.Value(orm.TextExpr(models.Users.Email))))))
var other = rows.UnionAllValues(otherInput, otherInput)
var invalid = rows.DistinctOnValueSet(combined, expr.Key(expr.AsSelected(expr.Row(other.Value()))))
