package invalid

import (
	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/decimal"
)

func invalid() { _ = intervalqueries.SampleFields().Period.Sum().Gt(decimal.Decimal{}) }
