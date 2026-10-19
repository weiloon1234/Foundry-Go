package invalid

import (
	"foundry.test/consumer/models"
	foundryquery "github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

var invalid = foundryquery.Sum(models.OrderFields().TotalCents).Gt(value.Null[decimal.Decimal]())
