package invalid

import (
	"foundry.test/consumer/intervalqueries"
)

func invalid() { _ = intervalqueries.SampleFields().Period.Like("%day%") }
