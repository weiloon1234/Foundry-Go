package invalid

import (
	"foundry.test/consumer/intervalqueries"
)

func invalid() { _ = intervalqueries.SampleDraft{}.SetPeriod("1 day") }
