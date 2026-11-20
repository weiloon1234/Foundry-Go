package aggregate_filter_nullable

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ = expr.GtValue(expr.Filter(expr.Sum(models.Orders.TotalCents), expr.Eq(expr.Value(models.Orders.TotalCents), 0)), value.Of(decimal.FromInt64(1)))
