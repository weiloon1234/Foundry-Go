package file_test

import (
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cache/file"
	"github.com/weiloon1234/Foundry-Go/internal/cachetest"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"testing"
	"time"
)

func TestContendedExpiryUsesCurrentTime(t *testing.T) {
	cachetest.RunContendedExpiry(t, func(t *testing.T) (cachetest.AtomicExpiryBackend, *testkit.Clock, cache.EntryKey) {
		source := testkit.NewClock(time.Now())
		config := file.DefaultConfig(t.TempDir())
		config.Clock = source
		return open(t, config), source, key(t, "contended")
	})
}
