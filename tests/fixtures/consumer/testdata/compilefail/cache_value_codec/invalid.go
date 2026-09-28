package invalid

import (
	"foundry.test/consumer/caching"
	"github.com/weiloon1234/Foundry-Go/cache"
)

var wrong = cache.Define[string, caching.Profile]("profiles", cache.StringKeys[string](), cache.JSON[string]())
