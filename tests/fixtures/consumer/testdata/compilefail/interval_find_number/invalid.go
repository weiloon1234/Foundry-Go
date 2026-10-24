package invalid

import (
	"context"
	"foundry.test/consumer/intervalqueries"
)

func invalid() { _, _ = intervalqueries.Labels.Find(context.Background(), nil, 1) }
