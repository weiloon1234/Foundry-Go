package memory_test

import (
	"context"
	"errors"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cache/memory"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func counterBackend(t *testing.T, extraBytes int) (*memory.Backend, cache.EntryKey, *testkit.Clock) {
	t.Helper()
	key, err := cache.NewEntryKey(cache.Namespace{Application: "counter", Environment: "test"}, "attempts", "key")
	if err != nil {
		t.Fatal(err)
	}
	config := memory.DefaultConfig()
	if extraBytes > 0 {
		config.MaxBytes = len(key.String()) + extraBytes
	}
	clock := testkit.NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	b, err := memory.New(config, clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := b.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return b, key, clock
}
func TestCounterCapacityFailurePreservesValueAndExpiry(t *testing.T) {
	b, key, clock := counterBackend(t, 1)
	if err := b.Put(t.Context(), key, []byte("9"), cache.For(time.Second)); err != nil {
		t.Fatal(err)
	}
	before := b.Stats()
	if value, err := b.Increment(t.Context(), key, 1, cache.Forever()); value != 0 || !errors.Is(err, fault.Invalid) {
		t.Fatal(value, err)
	}
	if data, found, err := b.Get(t.Context(), key); err != nil || !found || string(data) != "9" {
		t.Fatal(string(data), found, err)
	}
	if after := b.Stats(); after != before {
		t.Fatal("rejected counter evicted data", before, after)
	}
	clock.Advance(time.Second)
	if _, found, err := b.Get(t.Context(), key); err != nil || found {
		t.Fatal("rejected counter changed expiry", found, err)
	}
}
func TestCounterCorruptionIsRejectedWithoutMutation(t *testing.T) {
	for _, raw := range []string{"", "-0", "+1", "01", " 1", "1 ", "1.0", "1e2", "null", "9223372036854775808", "-9223372036854775809", "private payload"} {
		b, key, clock := counterBackend(t, 0)
		if err := b.Put(t.Context(), key, []byte(raw), cache.For(time.Second)); err != nil {
			t.Fatal(err)
		}
		if value, err := b.Increment(t.Context(), key, 1, cache.Forever()); value != 0 || !errors.Is(err, fault.Invalid) {
			t.Fatal(raw, value, err)
		}
		if data, found, err := b.Get(t.Context(), key); err != nil || !found || string(data) != raw {
			t.Fatal("corrupt data was overwritten", string(data), found, err)
		}
		clock.Advance(time.Second)
		if value, err := b.Increment(t.Context(), key, 1, cache.Forever()); err != nil || value != 1 {
			t.Fatal("expired corrupt data not replaced", value, err)
		}
	}
}
func TestCounterByteAccountingTracksDigitGrowthAndShrink(t *testing.T) {
	b, key, _ := counterBackend(t, 0)
	if value, err := b.Increment(t.Context(), key, 9, cache.Forever()); err != nil || value != 9 {
		t.Fatal(value, err)
	}
	for _, delta := range []int64{1, -1, -10, math.MinInt64 + 1, math.MaxInt64} {
		if _, err := b.Increment(t.Context(), key, delta, cache.Forever()); err != nil {
			t.Fatal(err)
		}
		data, found, err := b.Get(t.Context(), key)
		if err != nil || !found {
			t.Fatal(found, err)
		}
		if _, err := strconv.ParseInt(string(data), 10, 64); err != nil {
			t.Fatal(err)
		}
		if stats := b.Stats(); stats.Bytes != len(key.String())+len(data) || stats.Entries != 1 {
			t.Fatal(stats)
		}
	}
}
func TestCounterExactExpiryAtZeroTimeAndInvalidAdapterInputs(t *testing.T) {
	b, key, clock := counterBackend(t, 0)
	clock.Set(time.Time{}.Add(-time.Second))
	if _, err := b.Increment(t.Context(), key, 1, cache.For(time.Second)); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Second)
	if value, err := b.Increment(t.Context(), key, 2, cache.Forever()); err != nil || value != 2 {
		t.Fatal("zero instant became persistent", value, err)
	}
	if _, err := b.Increment(t.Context(), key, 1, cache.TTL{}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := b.Increment(nil, key, 1, cache.Forever()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := b.Increment(t.Context(), cache.EntryKey{}, 1, cache.Forever()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	var zero *memory.Backend
	if _, err := zero.Increment(t.Context(), key, 1, cache.Forever()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
