package invalid

import (
	"foundry.test/consumer/intervalqueries"
	foundryquery "github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	_ = intervalqueries.ProjectSummary(intervalqueries.QueryIntervalSamples()).SelectCount(foundryquery.Sum(intervalqueries.SampleFields().Period).Value())
}
