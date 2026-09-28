package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ = models.OrderFields().TotalCents.Sum().Filter(models.OrderFields().TotalCents.Gt(0)).Gt(value.Of(decimal.FromInt64(1)))
