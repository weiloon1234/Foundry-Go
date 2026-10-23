package aggregate_filter_nullable

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ = expr.Sum(models.Orders.TotalCents).Filter(expr.Value(models.Orders.TotalCents).Eq(0)).Gt(value.Of(decimal.FromInt64(1)))
