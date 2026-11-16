package invalid

import (
	"foundry.test/consumer/caching"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

var wrong = cache.Define[string, caching.Profile]("profiles", keyspace.StringKeys[string](), cache.JSON[string]())
