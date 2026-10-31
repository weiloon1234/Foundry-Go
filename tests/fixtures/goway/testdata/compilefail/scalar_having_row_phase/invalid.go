package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var selected = rows.SelectScalar(rows.From(models.Users.Query()), expr.AsSelected(expr.Count[models.User]()))
var invalid = rows.HavingScalar(selected, expr.Eq(expr.Value(orm.NumberExpr(models.Users.Age)), 1))
