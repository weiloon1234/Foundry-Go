package memory_test

import (
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cache/memory"
	"github.com/weiloon1234/Foundry-Go/internal/cachetest"
)

func TestMemoryEntryContract(t *testing.T) {
	cachetest.RunEntries(t, func(t *testing.T) cachetest.EntryFixture {
		b, _ := backend(t, memory.DefaultConfig())
		return cachetest.EntryFixture{Backend: b, Key: func(logical string) cache.EntryKey { return key(t, logical) }, Track: func(cache.EntryKey) {}, CorruptTagged: func(key cache.EntryKey) {
			if err := b.Put(t.Context(), key, []byte("corrupt envelope"), cache.Forever()); err != nil {
				t.Fatal(err)
			}
		}}
	})
}
func TestMemoryExpiryTransitionsPreservePayloadAndCounters(t *testing.T) {
	for _, tagged := range []bool{false, true} {
		name := "plain"
		if tagged {
			name = "tagged"
		}
		t.Run(name, func(t *testing.T) {
			b, clock := backend(t, memory.DefaultConfig())
			k := key(t, "expiry")
			snapshot := snapshot(t, b, k, key(t, "tag"))
			put := func(ttl cache.TTL) error {
				if tagged {
					return b.PutTagged(t.Context(), snapshot, []byte("41"), ttl)
				}
				return b.Put(t.Context(), k, []byte("41"), ttl)
			}
			expire := func(ttl cache.TTL) (bool, error) {
				if tagged {
					return b.ExpireTagged(t.Context(), snapshot, ttl)
				}
				return b.Expire(t.Context(), k, ttl)
			}
			exists := func() (bool, error) {
				if tagged {
					return b.ExistsTagged(t.Context(), snapshot)
				}
				return b.Exists(t.Context(), k)
			}
			increment := func() (int64, error) {
				if tagged {
					return b.IncrementTagged(t.Context(), snapshot, 1, cache.For(time.Hour))
				}
				return b.Increment(t.Context(), k, 1, cache.For(time.Hour))
			}
			if err := put(cache.For(time.Second)); err != nil {
				t.Fatal(err)
			}
			if ok, err := expire(cache.Forever()); err != nil || !ok {
				t.Fatal(ok, err)
			}
			clock.Advance(time.Hour)
			if value, err := increment(); err != nil || value != 42 {
				t.Fatal(value, err)
			}
			if ok, err := expire(cache.For(2 * time.Second)); err != nil || !ok {
				t.Fatal(ok, err)
			}
			clock.Advance(time.Second)
			if value, err := increment(); err != nil || value != 43 {
				t.Fatal(value, err)
			}
			clock.Advance(time.Second)
			if ok, err := exists(); err != nil || ok {
				t.Fatal("increment extended updated expiry", ok, err)
			}
			if ok, err := expire(cache.Forever()); err != nil || ok {
				t.Fatal("expired entry restored", ok, err)
			}
		})
	}
}
