package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

var invalid = models.OrderFields().TotalCents.Sum().Gt(value.Null[decimal.Decimal]())
