package invalid

import (
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/redis"
)

func wrong(client *redis.Client) {
	_, _ = cache.NewCoordinatedStore(client, cache.Config{}, cache.CoordinationConfig{})
}
