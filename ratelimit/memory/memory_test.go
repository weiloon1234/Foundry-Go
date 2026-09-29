package memory_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ratelimittest"
	"github.com/weiloon1234/Foundry-Go/internal/ratewindow"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	"github.com/weiloon1234/Foundry-Go/ratelimit/memory"
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

// windowStart returns the first window start of key at or after from.
func windowStart(key ratelimit.Key, window time.Duration, from int64) int64 {
	w, offset := window.Milliseconds(), key.WindowOffset(window).Milliseconds()
	start := from - (from-offset)%w
	if start < from {
		start += w
	}
	return start
}

func TestSharedContract(t *testing.T) {
	ratelimittest.Run(t, func(t *testing.T) (ratelimit.Backend, func(string) ratelimit.Key) {
		b, _, key := fixture(t, 100)
		return b, key
	})
}
func TestPhasedWindowsCapacityAndClockReversal(t *testing.T) {
	b, clock, key := fixture(t, 1)
	limit := ratelimit.PerSecond(2)
	first, second := key("first"), key("second")
	start := windowStart(first, time.Second, 10000)
	clock.set(start + 250)
	if d, err := b.RateLimit(t.Context(), first, limit, 2); err != nil || d.ResetAfter != 750*time.Millisecond {
		t.Fatal(d, err)
	}
	if _, err := b.RateLimit(t.Context(), second, limit, 1); !errors.Is(err, fault.Conflict) {
		t.Fatal("live bucket evicted", err)
	}
	if d, err := b.RateLimit(t.Context(), first, limit, 1); err != nil || d.Allowed {
		t.Fatal(d, err)
	}
	clock.set(start + 999)
	if d, err := b.RateLimit(t.Context(), first, limit, 1); err != nil || d.RetryAfter != time.Millisecond {
		t.Fatal(d, err)
	}
	// A backward step is clamped to the latest reading: no failure, no reopened quota.
	clock.set(start - 5000)
	if d, err := b.RateLimit(t.Context(), first, limit, 1); err != nil || d.Allowed || d.RetryAfter != time.Millisecond {
		t.Fatal("backward clock reopened quota or failed", d, err)
	}
	clock.set(start + 1000)
	d, err := b.RateLimit(t.Context(), second, ratelimit.PerSecond(3), 3)
	if err != nil || !d.Allowed || d.ResetAfter <= 0 || d.ResetAfter > time.Second {
		t.Fatal("expired bucket not reclaimed for capacity", d, err)
	}
	b.Close()
	if _, err := b.RateLimit(t.Context(), second, limit, 1); !errors.Is(err, fault.Closed) {
		t.Fatal(err)
	}
}
func TestWindowsArePhasedPerKey(t *testing.T) {
	b, clock, key := fixture(t, 64)
	clock.set(3_600_000 * 400)
	resets := make(map[time.Duration]bool)
	for i := range 32 {
		d, err := b.RateLimit(t.Context(), key(fmt.Sprintf("member-%d", i)), ratelimit.PerMinute(1), 1)
		if err != nil || !d.Allowed || d.ResetAfter <= 0 || d.ResetAfter > time.Minute {
			t.Fatal(d, err)
		}
		resets[d.ResetAfter] = true
	}
	if len(resets) < 16 {
		t.Fatal("windows still reset together", len(resets))
	}
}
func TestPolicyChangeConvertsLiveBucket(t *testing.T) {
	b, clock, key := fixture(t, 4)
	k := key("policy")
	start := windowStart(k, time.Minute, 600_000)
	clock.set(start + 1000)
	if d, err := b.RateLimit(t.Context(), k, ratelimit.PerMinute(10), 6); err != nil || !d.Allowed {
		t.Fatal(d, err)
	}
	// Lower capacity: usage is capped at the new capacity, so the key stays limited.
	if d, err := b.RateLimit(t.Context(), k, ratelimit.PerMinute(4), 1); err != nil || d.Allowed || d.Remaining != 0 {
		t.Fatal(d, err)
	}
	// Higher capacity with a new window: admitted usage still counts.
	d, err := b.RateLimit(t.Context(), k, ratelimit.PerHour(8), 2)
	if err != nil || !d.Allowed || d.Remaining != 0 || d.ResetAfter > time.Hour {
		t.Fatal(d, err)
	}
	if err := d.Validate(ratelimit.PerHour(8), 2); err != nil {
		t.Fatal(err)
	}
	if d, err := b.PeekRateLimit(t.Context(), k, ratelimit.PerHour(8), 1); err != nil || d.Allowed || d.Remaining != 0 {
		t.Fatal("conversion was not persisted", d, err)
	}
}
func TestFullTableReclaimsExpiredInEndOrder(t *testing.T) {
	b, clock, key := fixture(t, 3)
	// Choose a time where the hour and minute buckets outlive the one-second bucket.
	base := int64(1_000_000)
	for ; ; base += 1000 {
		hour, _ := ratewindow.End(base, time.Hour.Milliseconds(), key("0").WindowOffset(time.Hour).Milliseconds())
		minute, _ := ratewindow.End(base, time.Minute.Milliseconds(), key("2").WindowOffset(time.Minute).Milliseconds())
		if hour > base+2000 && minute > base+2000 {
			break
		}
	}
	clock.set(base)
	for i, limit := range []ratelimit.Limit{ratelimit.PerHour(1), ratelimit.PerSecond(1), ratelimit.PerMinute(1)} {
		if _, err := b.RateLimit(t.Context(), key(fmt.Sprint(i)), limit, 1); err != nil {
			t.Fatal(err)
		}
	}
	// Only the one-second bucket has expired; the hour and minute buckets are live.
	clock.set(base + 1000)
	if _, err := b.RateLimit(t.Context(), key("new"), ratelimit.PerHour(1), 1); err != nil {
		t.Fatal("expired bucket not reclaimed", err)
	}
	if _, err := b.RateLimit(t.Context(), key("another"), ratelimit.PerHour(1), 1); !errors.Is(err, fault.Conflict) {
		t.Fatal("live bucket evicted", err)
	}
	if d, err := b.RateLimit(t.Context(), key("0"), ratelimit.PerHour(1), 1); err != nil || d.Allowed {
		t.Fatal("live bucket lost usage", d, err)
	}
	if cleared, err := b.ClearRateLimit(t.Context(), key("0")); err != nil || !cleared {
		t.Fatal(cleared, err)
	}
	if _, err := b.RateLimit(t.Context(), key("another"), ratelimit.PerHour(1), 1); err != nil {
		t.Fatal("cleared bucket still occupied capacity", err)
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
	clock.set(3000)
	if d, err := b.RateLimit(t.Context(), k, ratelimit.PerSecond(2), 2); err != nil || !d.Allowed {
		t.Fatal(d, err)
	}
}
func BenchmarkNewKeysAtCapacity(b *testing.B) {
	clock := &controlledClock{now: time.UnixMilli(1_000_000)}
	backend, err := memory.New(10000, clock)
	if err != nil {
		b.Fatal(err)
	}
	keys := make([]ratelimit.Key, b.N+10000)
	for i := range keys {
		keys[i], _ = ratelimit.NewKey(keyspace.Namespace{Application: "bench", Environment: "local"}, "requests", fmt.Sprint(i))
	}
	for i := range 10000 {
		if _, err := backend.RateLimit(b.Context(), keys[i], ratelimit.PerSecond(1), 1); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := range b.N {
		// Every second the whole previous generation expires and is reclaimed.
		clock.set(1_000_000 + int64(i/10000+1)*1000)
		if _, err := backend.RateLimit(b.Context(), keys[10000+i], ratelimit.PerSecond(1), 1); err != nil {
			b.Fatal(err)
		}
	}
}

// Tightening a policy after the quota is spent keeps the key limited for the
// new, longer window, even though the conversion happened on a denial.
func TestDeniedPolicyConversionKeepsLaterExpiry(t *testing.T) {
	b, clock, key := fixture(t, 4)
	k := key("tightened")
	// A minute window at the start of an hour window, so the hour ends later.
	start := windowStart(k, time.Minute, windowStart(k, time.Hour, 600_000))
	clock.set(start + 1000)
	if d, err := b.RateLimit(t.Context(), k, ratelimit.PerMinute(10), 10); err != nil || !d.Allowed {
		t.Fatal(d, err)
	}
	d, err := b.RateLimit(t.Context(), k, ratelimit.PerHour(10), 1)
	if err != nil || d.Allowed || d.RetryAfter <= time.Minute {
		t.Fatal("converted denial did not use the hour window", d, err)
	}
	// The old minute window has ended; the converted hour bucket still holds.
	clock.set(start + 2*time.Minute.Milliseconds())
	if d, err := b.RateLimit(t.Context(), k, ratelimit.PerHour(10), 1); err != nil || d.Allowed {
		t.Fatal("quota reopened when the old window ended", d, err)
	}
	// A peek with another policy still persists nothing.
	if d, err := b.PeekRateLimit(t.Context(), k, ratelimit.PerMinute(20), 1); err != nil || !d.Allowed {
		t.Fatal(d, err)
	}
	if d, err := b.RateLimit(t.Context(), k, ratelimit.PerHour(10), 1); err != nil || d.Allowed {
		t.Fatal("peek changed the converted bucket", d, err)
	}
}
