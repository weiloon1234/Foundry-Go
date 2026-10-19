package invalid

import (
	"foundry.test/consumer/models"
	foundryquery "github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ = foundryquery.Sum(models.OrderFields().TotalCents).Filter(models.OrderFields().TotalCents.Gt(0)).Gt(value.Of(decimal.FromInt64(1)))
