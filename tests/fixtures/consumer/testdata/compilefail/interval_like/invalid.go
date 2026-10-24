package invalid

import (
	"foundry.test/consumer/intervalqueries"
)

func invalid() { _ = intervalqueries.Samples.Period.Like("%day%") }
