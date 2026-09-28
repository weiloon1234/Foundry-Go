package invalid

import (
	"context"
	"foundry.test/consumer/caching"
	"github.com/weiloon1234/Foundry-Go/cache"
)

func wrong(c caching.Profiles) { _, _ = c.Expire(context.Background(), "raw-member", cache.Forever()) }
