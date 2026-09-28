package postgres_test

import (
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/internal/cachetest"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"testing"
)

func TestContendedExpiryUsesCurrentTime(t *testing.T) {
	cachetest.RunContendedExpiry(t, func(t *testing.T) (cachetest.AtomicExpiryBackend, *testkit.Clock, cache.EntryKey) {
		backend, source := prepare(t)
		return backend, source, key(t, "contended")
	})
}
