package memory_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cache/memory"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func key(t *testing.T, logical string) cache.EntryKey {
	t.Helper()
	k, err := cache.NewEntryKey(cache.Namespace{Application: "memory", Environment: "test"}, "values", logical)
	if err != nil {
		t.Fatal(err)
	}
	return k
}
func backend(t *testing.T, config memory.Config) (*memory.Backend, *testkit.Clock) {
	t.Helper()
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
	return b, clock
}
func TestBackendOwnsBytesIncludingEmptyHits(t *testing.T) {
	b, _ := backend(t, memory.DefaultConfig())
	k := key(t, "one")
	for _, data := range [][]byte{nil, {}, []byte("value")} {
		if err := b.Put(t.Context(), k, data, cache.Forever()); err != nil {
			t.Fatal(err)
		}
		if len(data) > 0 {
			data[0] = 'X'
		}
		got, found, err := b.Get(t.Context(), k)
		if err != nil || !found {
			t.Fatal("empty hit became miss", found, err)
		}
		if len(got) > 0 {
			if string(got) != "value" {
				t.Fatal("put buffer retained")
			}
			got[0] = 'Y'
			again, _, _ := b.Get(t.Context(), k)
			if string(again) != "value" {
				t.Fatal("read buffer retained")
			}
		}
	}
}
func TestAtomicAddPreservesExistingTTLAndReacquiresExpiredKey(t *testing.T) {
	b, clock := backend(t, memory.DefaultConfig())
	k := key(t, "one")
	var wins atomic.Int64
	var work sync.WaitGroup
	for range 64 {
		work.Go(func() {
			added, err := b.Add(t.Context(), k, []byte("value"), cache.For(time.Second))
			if err != nil {
				t.Error(err)
			}
			if added {
				wins.Add(1)
			}
		})
	}
	work.Wait()
	if wins.Load() != 1 {
		t.Fatal("add was not atomic", wins.Load())
	}
	clock.Advance(500 * time.Millisecond)
	if added, err := b.Add(t.Context(), k, []byte("replacement"), cache.For(time.Hour)); err != nil || added {
		t.Fatal("add replaced live key", added, err)
	}
	clock.Advance(500 * time.Millisecond)
	if _, found, err := b.Get(t.Context(), k); err != nil || found {
		t.Fatal("failed add extended TTL")
	}
	if added, err := b.Add(t.Context(), k, []byte("new"), cache.For(time.Nanosecond)); err != nil || !added {
		t.Fatal("expired entry blocked add")
	}
	clock.Advance(time.Nanosecond)
	if removed, err := b.Forget(t.Context(), k); err != nil || removed {
		t.Fatal("expired key reported removed")
	}
	// Finite expiry at time.Time{} must never become the persistent sentinel.
	clock.Set(time.Time{}.Add(-time.Second))
	if err := b.Put(t.Context(), k, []byte("finite"), cache.For(time.Second)); err != nil {
		t.Fatal(err)
	}
	clock.Set(time.Time{})
	if _, found, _ := b.Get(t.Context(), k); found {
		t.Fatal("zero instant became permanent expiry")
	}
}
func TestCapacityUsesLRUAndReclaimsExpiryBeforeLiveEntries(t *testing.T) {
	b, clock := backend(t, memory.Config{MaxEntries: 2, MaxBytes: 4096})
	a, c, d := key(t, "a"), key(t, "c"), key(t, "d")
	if err := b.Put(t.Context(), a, []byte("a"), cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if err := b.Put(t.Context(), c, []byte("c"), cache.For(time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Nanosecond)
	if err := b.Put(t.Context(), d, []byte("d"), cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := b.Get(t.Context(), a); !found {
		t.Fatal("live LRU evicted before expired entry")
	}
	if b.Stats().Evictions != 0 {
		t.Fatal("expiration counted as capacity eviction")
	}
	if err := b.Put(t.Context(), c, []byte("c"), cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := b.Get(t.Context(), d); found {
		t.Fatal("least recently used entry retained")
	}
	if _, found, _ := b.Get(t.Context(), a); !found {
		t.Fatal("recently read entry evicted")
	}
	if stats := b.Stats(); stats.Entries != 2 || stats.Evictions != 1 || stats.Bytes > 4096 {
		t.Fatal("capacity stats", stats)
	}
}
func TestByteBudgetAndRejectedReplacementRetainExistingValue(t *testing.T) {
	k := key(t, "one")
	cost := len(k.String()) + 8
	b, _ := backend(t, memory.Config{MaxEntries: 10, MaxBytes: cost})
	if err := b.Put(t.Context(), k, []byte("original"), cache.Forever()); err != nil {
		t.Fatal(err)
	}
	before := b.Stats()
	if err := b.Put(t.Context(), k, make([]byte, 9), cache.Forever()); !errors.Is(err, fault.Invalid) {
		t.Fatal("oversized replacement accepted", err)
	}
	if got, found, _ := b.Get(t.Context(), k); !found || string(got) != "original" {
		t.Fatal("rejected write lost prior value")
	}
	if after := b.Stats(); after != before {
		t.Fatal("rejected write changed budget", after, before)
	}
	other := key(t, "other")
	if err := b.Put(t.Context(), other, []byte("next"), cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if stats := b.Stats(); stats.Bytes > cost || stats.Entries != 1 || stats.Evictions != 1 {
		t.Fatal("byte budget not enforced", stats)
	}
}
func TestNamespaceIsolationCancellationAndClose(t *testing.T) {
	b, _ := backend(t, memory.DefaultConfig())
	a := key(t, "one")
	other, err := cache.NewEntryKey(cache.Namespace{Application: "other", Environment: "test"}, "values", "one")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Put(t.Context(), a, []byte("private"), cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if _, found, err := b.Get(t.Context(), other); err != nil || found {
		t.Fatal("cross-namespace read")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := b.Put(canceled, a, []byte("changed"), cache.Forever()); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled write accepted")
	}
	if err := b.Close(canceled); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled close accepted")
	}
	if got, found, _ := b.Get(t.Context(), a); !found || string(got) != "private" {
		t.Fatal("canceled operation changed state")
	}
	if err := b.Put(t.Context(), a, nil, cache.TTL{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("implicit persistent TTL accepted")
	}
	if _, _, err := b.Get(t.Context(), cache.EntryKey{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid entry key accepted")
	}
	for range 2 {
		if err := b.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := b.Get(t.Context(), a); !errors.Is(err, fault.Closed) {
		t.Fatal("closed backend read accepted")
	}
	if _, err := b.Add(t.Context(), a, nil, cache.Forever()); !errors.Is(err, fault.Closed) {
		t.Fatal("closed backend write accepted")
	}
	if stats := b.Stats(); !stats.Closed || stats.Entries != 0 || stats.Bytes != 0 {
		t.Fatal("close retained memory", stats)
	}
}
func TestConstructorRejectsUnboundedMemory(t *testing.T) {
	clock := testkit.NewClock(time.Time{})
	for _, config := range []memory.Config{{}, {MaxEntries: 1}, {MaxBytes: 10}, {MaxEntries: -1, MaxBytes: 10}} {
		if _, err := memory.New(config, clock); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid memory limits accepted")
		}
	}
	if _, err := memory.New(memory.DefaultConfig(), nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("missing clock accepted")
	}
}
