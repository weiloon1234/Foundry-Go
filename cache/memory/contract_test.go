package memory_test

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cache/memory"
	"github.com/weiloon1234/Foundry-Go/internal/cachetest"
)

func TestSharedCacheContract(t *testing.T) {
	cachetest.Run(t, func(t *testing.T) (cachetest.Backend, func(string) cache.EntryKey) {
		b, _ := backend(t, memory.DefaultConfig())
		return b, func(logical string) cache.EntryKey { return key(t, logical) }
	})
}

func TestSharedTaggedCacheContract(t *testing.T) {
	cachetest.RunTagged(t, func(t *testing.T) cachetest.TaggedFixture {
		b, _ := backend(t, memory.DefaultConfig())
		return cachetest.TaggedFixture{Backend: b, Key: func(logical string) cache.EntryKey { return key(t, logical) }, Track: func(cache.EntryKey) {}}
	})
}
