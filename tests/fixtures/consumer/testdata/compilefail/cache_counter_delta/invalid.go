package invalid

import (
	"context"
	"foundry.test/consumer/caching"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(counts caching.ViewCounts, id model.ID[mutatorqueries.Member], delta float64) {
	_, _ = counts.Increment(context.Background(), id, delta, cache.Forever())
}
