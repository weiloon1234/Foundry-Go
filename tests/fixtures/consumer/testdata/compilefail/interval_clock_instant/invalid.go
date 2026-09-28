package invalid

import (
	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	_ = query.InstantDifference(intervalqueries.SampleFields().ClockStart, intervalqueries.SampleFields().ClockEnd)
}
