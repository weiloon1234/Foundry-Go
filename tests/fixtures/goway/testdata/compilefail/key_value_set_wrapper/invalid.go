package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var scalar = rows.Values(rows.SelectScalar(rows.From(models.Users.Query()), expr.Row(expr.Value(orm.NumberExpr(models.Users.Age))).Value()))
var combined = rows.UnionAllValues(scalar, scalar)

type forged struct{ rows.ValueSetQuery[int] }

var invalid = rows.DistinctOnValueSet(forged{combined}, expr.Row(combined.Value()).Value().Key())
