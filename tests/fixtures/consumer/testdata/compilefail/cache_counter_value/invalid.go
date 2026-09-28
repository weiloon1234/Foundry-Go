package invalid

import (
	"context"
	"foundry.test/consumer/caching"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(counts caching.ViewCounts, id model.ID[mutatorqueries.Member], value string) {
	_ = counts.Put(context.Background(), id, value, cache.Forever())
}
