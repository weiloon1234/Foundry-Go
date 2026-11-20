package aggregate_phase_to_row

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
)

var _ = expr.NullableOrderedRowExpression[models.Order, decimal.Decimal](expr.Sum(models.Orders.TotalCents))
