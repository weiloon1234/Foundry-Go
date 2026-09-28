package invalid

import (
	"foundry.test/consumer/intervalqueries"
)

func invalid() {
	_ = intervalqueries.ProjectSummary(intervalqueries.QueryIntervalSamples()).SelectCount(intervalqueries.SampleFields().Period.Sum().Value())
}
