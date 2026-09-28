package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/lease"
	"time"
)

func wrong(b lease.Backend, k cache.EntryKey, owner lease.Owner) {
	_, _ = b.LeaseAcquire(context.Background(), k, owner, time.Second)
}
