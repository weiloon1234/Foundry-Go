package subquery_scalar_left_join

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type rightSide struct{}

var left = rows.SelectScalar(rows.From(models.Users.Query().Where(orm.NumberGt(models.Users.Age, 18)).Limit(1).Offset(1)), expr.Row(expr.Value(orm.NumberExpr(models.Users.Age))).Value())
var right = rows.As[rightSide](models.Users.Query(), "right_user")
var on = rows.OnEqual(expr.Value(orm.NumberExpr(models.Users.Age)), expr.At(rows.Scope(right), orm.NumberExpr(models.Users.Age)))
var _ = rows.LeftJoin[models.User, rows.Alias[rightSide, models.User]](left, right, on)
