package invalid

import (
	"foundry.test/consumer/intervalqueries"
	foundryquery "github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
)

func invalid() { _ = foundryquery.Sum(intervalqueries.SampleFields().Period).Gt(decimal.Decimal{}) }
