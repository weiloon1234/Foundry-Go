package redis

import (
	"github.com/weiloon1234/Foundry-Go/internal/cachetest"
	"testing"
)

func TestRedisSharedTaggedCacheContract(t *testing.T) {
	cachetest.RunTagged(t, func(t *testing.T) cachetest.TaggedFixture {
		client, key, track := integrationTracked(t, nil)
		return cachetest.TaggedFixture{Backend: client, Key: key, Track: track}
	})
}
