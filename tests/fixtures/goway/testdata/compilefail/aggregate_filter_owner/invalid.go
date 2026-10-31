package aggregate_filter_owner

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Filter[models.User](expr.Sum(orm.NumberExpr(models.Orders.TotalCents)), expr.Eq(expr.Value(orm.NumberExpr(models.Users.Age)), 18))
