package invalid

import (
	"foundry.test/consumer/caching"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/model"
)

var wrong = cache.Define[model.ID[mutatorqueries.Member], caching.Profile]("profiles", cache.StringKeys[string](), cache.JSON[caching.Profile]())
