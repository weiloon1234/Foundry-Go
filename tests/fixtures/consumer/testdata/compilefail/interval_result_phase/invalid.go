package invalid

import (
	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func invalid() {
	_ = intervalqueries.QueryIntervalSamples().Where(intervalqueries.SampleFields().Period.Sum().Gt(temporal.Days(1)))
}
