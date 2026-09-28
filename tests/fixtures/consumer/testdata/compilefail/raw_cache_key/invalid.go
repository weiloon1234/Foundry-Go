package invalid

import (
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/redis/raw"
)

func wrong(k cache.EntryKey) { _ = raw.NewCommand("GET", raw.DecodeString()).Key(k) }
