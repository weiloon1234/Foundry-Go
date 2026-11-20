package aggregate_filter_owner

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Filter[models.User](expr.Sum(models.Orders.TotalCents), expr.Eq(expr.Value(models.Users.Age), 18))
