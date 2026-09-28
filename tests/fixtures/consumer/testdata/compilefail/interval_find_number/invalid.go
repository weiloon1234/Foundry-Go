package invalid

import (
	"context"
	"foundry.test/consumer/intervalqueries"
)

func invalid() { _, _ = intervalqueries.QueryIntervalLabels().Find(context.Background(), nil, 1) }
