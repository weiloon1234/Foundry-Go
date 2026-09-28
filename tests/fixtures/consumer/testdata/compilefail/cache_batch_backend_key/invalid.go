package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/lease"
)

func wrong(b cache.BatchBackend, keys []lease.Key) { _, _ = b.ForgetMany(context.Background(), keys) }
