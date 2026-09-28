package invalid

import (
	"context"
	"foundry.test/consumer/caching"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(c caching.Profiles, id model.ID[mutatorqueries.Member]) {
	_ = c.Put(context.Background(), id, "wrong payload", cache.Forever())
}
