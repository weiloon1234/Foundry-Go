package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/lease"
)

func wrong(b cache.CoordinatedBackend, k cache.EntryKey, o lease.Owner) {
	_ = b.PutLeased(context.Background(), k, nil, cache.Forever(), o)
}
