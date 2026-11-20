package having_nullable

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ = expr.GtValue(expr.Sum(models.Orders.TotalCents), value.Null[decimal.Decimal]())
