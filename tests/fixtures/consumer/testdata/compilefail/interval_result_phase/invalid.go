package invalid

import (
	"foundry.test/consumer/intervalqueries"
	foundryquery "github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func invalid() {
	_ = intervalqueries.QueryIntervalSamples().Where(foundryquery.Sum(intervalqueries.SampleFields().Period).Gt(temporal.Days(1)))
}
