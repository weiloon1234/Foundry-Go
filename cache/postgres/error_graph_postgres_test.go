package postgres_test

import (
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/cache"
	cachepg "github.com/weiloon1234/Foundry-Go/cache/postgres"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
)

func TestPostgresCacheIncrementDoesNotRecoverCapacityFromHostileDatabaseError(t *testing.T) {
	var armed atomic.Pointer[errorgraphtest.Error]
	var calls atomic.Int32
	f := extensiontest.OpenWithConnector(t, cachepg.Migrations(), func(base driver.Connector) driver.Connector {
		return extensiontest.InterceptQueries(base, func(statement string) error {
			if cause := armed.Load(); cause != nil && strings.Contains(statement, "FOR UPDATE") {
				calls.Add(1)
				return cause
			}
			return nil
		})
	})
	config := cachepg.DefaultConfig()
	config.Schema = f.Schema
	config.PruneInterval = 0
	backend := open(t, f.DB, config)
	k := key(t, "hostile")
	if err := backend.Put(t.Context(), k, []byte("1"), cache.Forever()); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		cause := &errorgraphtest.Error{Mode: mode}
		calls.Store(0)
		armed.Store(cause)
		_, err := backend.Increment(t.Context(), k, 1, cache.Forever())
		armed.Store(nil)
		if err == nil || !errors.Is(err, cause) || calls.Load() != 1 || cause.Formatted.Load() != 0 || cause.Visits.Load() > 2048 {
			t.Fatal("cache driver failure retried, lost identity, or traversed without bounds", calls.Load(), cause.Visits.Load())
		}
	}
}
