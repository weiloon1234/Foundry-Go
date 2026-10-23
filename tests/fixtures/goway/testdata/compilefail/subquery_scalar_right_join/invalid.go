package subquery_scalar_right_join

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type rightSide struct{}

var left = rows.SelectScalar(rows.From(models.Users.Query().Where(models.Users.Age.Gt(18)).Limit(1).Offset(1)), expr.Select(expr.Value(models.Users.Age)))
var right = rows.As[rightSide](models.Users.Query(), "right_user")
var on = rows.OnEqual(expr.Value(models.Users.Age), expr.At(rows.Scope(right), models.Users.Age))
var _ = rows.RightJoin[models.User, rows.Alias[rightSide, models.User]](left, right, on)
