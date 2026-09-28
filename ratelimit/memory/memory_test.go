package memory_test

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ratelimittest"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	"github.com/weiloon1234/Foundry-Go/ratelimit/memory"
	"sync"
	"testing"
	"time"
)

type controlledClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *controlledClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *controlledClock) set(ms int64)   { c.mu.Lock(); defer c.mu.Unlock(); c.now = time.UnixMilli(ms) }
func fixture(t *testing.T, capacity int) (*memory.Backend, *controlledClock, func(string) ratelimit.Key) {
	t.Helper()
	clock := &controlledClock{now: time.UnixMilli(1250)}
	b, err := memory.New(capacity, clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	return b, clock, func(text string) ratelimit.Key {
		k, err := ratelimit.NewKey(keyspace.Namespace{Application: "test", Environment: "local"}, "requests", text)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
}
func TestSharedContract(t *testing.T) {
	ratelimittest.Run(t, func(t *testing.T) (ratelimit.Backend, func(string) ratelimit.Key) {
		b, _, key := fixture(t, 100)
		return b, key
	})
}
func TestExpiryCapacityAndClockReversal(t *testing.T) {
	b, clock, key := fixture(t, 1)
	limit := ratelimit.PerSecond(2)
	if d, err := b.RateLimit(t.Context(), key("first"), limit, 2); err != nil || d.ResetAfter != 750*time.Millisecond {
		t.Fatal(d, err)
	}
	if _, err := b.RateLimit(t.Context(), key("second"), limit, 1); !errors.Is(err, fault.Conflict) {
		t.Fatal(err)
	}
	if d, err := b.RateLimit(t.Context(), key("first"), limit, 1); err != nil || d.Allowed {
		t.Fatal(d, err)
	}
	clock.set(1999)
	if d, err := b.RateLimit(t.Context(), key("first"), limit, 1); err != nil || d.RetryAfter != time.Millisecond {
		t.Fatal(d, err)
	}
	clock.set(2000)
	if d, err := b.RateLimit(t.Context(), key("second"), ratelimit.PerSecond(3), 3); err != nil || !d.Allowed || d.ResetAfter != time.Second {
		t.Fatal(d, err)
	}
	clock.set(1999)
	if d, err := b.RateLimit(t.Context(), key("second"), limit, 1); !errors.Is(err, fault.Conflict) || d.Allowed {
		t.Fatal(d, err)
	}
	clock.set(2000)
	if d, err := b.RateLimit(t.Context(), key("second"), ratelimit.PerSecond(3), 1); err != nil || d.Allowed {
		t.Fatal(d, err)
	}
	b.Close()
	if _, err := b.RateLimit(t.Context(), key("second"), limit, 1); !errors.Is(err, fault.Closed) {
		t.Fatal(err)
	}
}
func TestNamespaceAndExpiredPolicy(t *testing.T) {
	b, clock, key := fixture(t, 3)
	k := key("same")
	other, _ := ratelimit.NewKey(keyspace.Namespace{Application: "other", Environment: "local"}, "requests", "same")
	for _, k := range []ratelimit.Key{k, other} {
		if d, err := b.RateLimit(t.Context(), k, ratelimit.PerSecond(1), 1); err != nil || !d.Allowed {
			t.Fatal(d, err)
		}
	}
	clock.set(2000)
	if d, err := b.RateLimit(t.Context(), k, ratelimit.PerSecond(2), 2); err != nil || !d.Allowed {
		t.Fatal(d, err)
	}
}
