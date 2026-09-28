package memory_test

import (
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cache/memory"
	"testing"
	"time"
)

func TestExpiryPressurePreservesLiveLRU(t *testing.T) {
	for _, change := range []string{"earlier", "later", "persistent", "zero-instant", "replacement"} {
		t.Run(change, func(t *testing.T) {
			b, clock := backend(t, memory.Config{MaxEntries: 2, MaxBytes: 4096})
			if change == "zero-instant" {
				clock.Set(time.Time{}.Add(-time.Second))
			}
			live, expires, next := key(t, "live"), key(t, "expires"), key(t, "next")
			put := func(k cache.EntryKey, ttl cache.TTL) {
				t.Helper()
				if err := b.Put(t.Context(), k, []byte("value"), ttl); err != nil {
					t.Fatal(err)
				}
			}
			put(live, cache.Forever())
			put(expires, cache.For(time.Hour))
			if change == "persistent" {
				put(expires, cache.Forever())
			}
			if change == "later" {
				if _, err := b.Expire(t.Context(), expires, cache.For(time.Nanosecond)); err != nil {
					t.Fatal(err)
				}
			}
			if change == "replacement" {
				put(expires, cache.For(time.Second))
			} else {
				if found, err := b.Expire(t.Context(), expires, cache.For(time.Second)); err != nil || !found {
					t.Fatal(found, err)
				}
			}
			clock.Advance(time.Second)
			put(next, cache.Forever())
			if _, found, err := b.Get(t.Context(), live); err != nil || !found {
				t.Fatalf("live LRU evicted before expired entry: %v %v", found, err)
			}
			if stats := b.Stats(); stats.Evictions != 0 || stats.Entries != 2 {
				t.Fatal(stats)
			}
		})
	}
}

func TestExpiryPressureTracksNextDeadlineAfterSweep(t *testing.T) {
	b, clock := backend(t, memory.Config{MaxEntries: 3, MaxBytes: 4096})
	for i, name := range []string{"first", "second", "third"} {
		if err := b.Put(t.Context(), key(t, name), nil, cache.For(time.Duration(i+1)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	for remaining := 2; remaining >= 0; remaining-- {
		clock.Advance(time.Second)
		if stats := b.Stats(); stats.Entries != remaining || stats.Evictions != 0 {
			t.Fatal(stats)
		}
	}
}
