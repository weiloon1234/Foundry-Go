package invalid

import (
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func invalid(stores *cache.Stores) { _, _ = stores.Store(storage.DiskID("default")) }
