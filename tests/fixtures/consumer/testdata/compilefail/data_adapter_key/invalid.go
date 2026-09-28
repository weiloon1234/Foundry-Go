package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/redis/data"
)

func wrong(b data.Backend, k cache.EntryKey) { _, _ = b.DataExists(context.Background(), k) }
