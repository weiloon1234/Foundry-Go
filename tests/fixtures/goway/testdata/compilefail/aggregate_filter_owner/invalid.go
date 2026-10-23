package aggregate_filter_owner

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Sum(models.Orders.TotalCents).Filter(expr.Value(models.Users.Age).Eq(18))
