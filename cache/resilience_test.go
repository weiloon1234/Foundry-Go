package cache_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	cachememory "github.com/weiloon1234/Foundry-Go/cache/memory"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/lease"
	leasememory "github.com/weiloon1234/Foundry-Go/lease/memory"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

// racingInvalidation rotates the namespace immediately before the first tagged
// reads, modeling an invalidation that lands between snapshot and read.
type racingInvalidation struct {
	*cachememory.Backend
	remaining atomic.Int32
	namespace cache.EntryKey
}

func (b *racingInvalidation) GetTagged(ctx context.Context, key cache.TaggedKey) ([]byte, bool, error) {
	if b.remaining.Add(-1) >= 0 {
		if err := b.Backend.InvalidateTags(ctx, []cache.EntryKey{b.namespace}); err != nil {
			return nil, false, err
		}
	}
	return b.Backend.GetTagged(ctx, key)
}
func (b *racingInvalidation) ExistsTagged(ctx context.Context, key cache.TaggedKey) (bool, error) {
	if b.remaining.Add(-1) >= 0 {
		if err := b.Backend.InvalidateTags(ctx, []cache.EntryKey{b.namespace}); err != nil {
			return false, err
		}
	}
	return b.Backend.ExistsTagged(ctx, key)
}

func TestTaggedReadsRacingInvalidationNeverConflict(t *testing.T) {
	_, backend, _ := store(t, nil)
	scope, err := cache.NewNamespaceTagKey(namespace)
	if err != nil {
		t.Fatal(err)
	}
	racing := &racingInvalidation{Backend: backend, namespace: scope}
	s, err := cache.NewStore(racing, cache.DefaultConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	c, err := profiles.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Put(t.Context(), "key", profile{Name: "old"}, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	// One race: the read re-resolves the new snapshot and reports the miss.
	racing.remaining.Store(1)
	if _, found, err := c.Get(t.Context(), "key"); err != nil || found {
		t.Fatal("pure read failed or returned invalidated data", found, err)
	}
	if err := c.Put(t.Context(), "key", profile{Name: "current"}, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	// Persistent races exhaust the bounded retries and become a miss, not Conflict.
	racing.remaining.Store(10)
	if _, found, err := c.Get(t.Context(), "key"); err != nil || found {
		t.Fatal(found, err)
	}
	racing.remaining.Store(10)
	if found, err := c.Exists(t.Context(), "key"); err != nil || found {
		t.Fatal(found, err)
	}
	racing.remaining.Store(1)
	loaded, err := c.Remember(t.Context(), "key", cache.Forever(), func(context.Context) (profile, error) { return profile{Name: "loaded"}, nil })
	if err != nil || loaded.Name != "loaded" {
		t.Fatal("Remember failed on a racing snapshot", loaded, err)
	}
	if stats := s.Stats(); stats.SnapshotRetries == 0 {
		t.Fatal("snapshot retries were not counted", stats)
	}
}

// overloadedBackend reports a framework capacity failure from every read.
type overloadedBackend struct{ cache.Backend }

func (overloadedBackend) Get(context.Context, cache.EntryKey) ([]byte, bool, error) {
	return nil, false, fault.New(fault.Overloaded, "adapter capacity is exhausted")
}

func TestCacheFailuresKeepTheirFrameworkClassification(t *testing.T) {
	_, backend, _ := store(t, nil)
	s, err := cache.NewStore(overloadedBackend{backend}, cache.DefaultConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	c, err := profiles.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = c.Get(t.Context(), "key")
	var classified *fault.Error
	if !errors.Is(err, fault.Overloaded) || errors.Is(err, fault.Internal) || !errors.As(err, &classified) || classified.Code() != fault.Overloaded {
		t.Fatal("capacity failure was reclassified", err)
	}
}

func TestOverBoundStoredValuesAreMissesThatWritesReplace(t *testing.T) {
	large, backend, _ := store(t, nil)
	c, err := profiles.Bind(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Put(t.Context(), "key", profile{Name: strings.Repeat("written under a larger bound ", 4)}, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	config := cache.DefaultConfig(namespace)
	config.MaxValueBytes = 64
	small, err := cache.NewStore(backend, config)
	if err != nil {
		t.Fatal(err)
	}
	narrow, err := profiles.Bind(small)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := narrow.Get(t.Context(), "key"); err != nil || found {
		t.Fatal("over-bound value was not a miss", found, err)
	}
	got, err := narrow.Remember(t.Context(), "key", cache.Forever(), func(context.Context) (profile, error) { return profile{Name: "n"}, nil })
	if err != nil || got.Name != "n" {
		t.Fatal("over-bound value could not be replaced", got, err)
	}
	if removed, err := narrow.Forget(t.Context(), "key"); err != nil || !removed {
		t.Fatal(removed, err)
	}
}

// flushOnly is a basic adapter with physical namespace removal and no tags.
type flushOnly struct {
	cache.Backend
	flushed []cache.Namespace
}

func (b *flushOnly) FlushNamespace(_ context.Context, namespace cache.Namespace) (uint64, error) {
	b.flushed = append(b.flushed, namespace)
	return 3, nil
}

func TestInvalidateFlushesAdaptersWithoutTags(t *testing.T) {
	_, backend, _ := store(t, nil)
	plain := &flushOnly{Backend: struct{ cache.Backend }{backend}}
	s, err := cache.NewStore(plain, cache.DefaultConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Invalidate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(plain.flushed) != 1 || plain.flushed[0] != namespace {
		t.Fatal("namespace flush was not requested", plain.flushed)
	}
	unsupported, err := cache.NewStore(struct{ cache.Backend }{backend}, cache.DefaultConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	if err := unsupported.Invalidate(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}

func TestStoreStatsCountTypedOperations(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _, _ := store(t, nil)
		c, err := profiles.Bind(s)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := c.Get(t.Context(), "missing"); err != nil {
			t.Fatal(err)
		}
		release := make(chan struct{})
		owner := asyncRemember(c, t.Context(), "key", func(context.Context) (profile, error) { <-release; return profile{}, nil })
		synctest.Wait()
		follower := asyncRemember(c, t.Context(), "key", noLoad)
		synctest.Wait()
		close(release)
		<-owner
		<-follower
		if _, _, err := c.Get(t.Context(), "key"); err != nil {
			t.Fatal(err)
		}
		stats := s.Stats()
		if stats.Hits != 1 || stats.Misses != 3 || stats.Writes != 1 || stats.Loads != 1 || stats.Coalesced != 1 || stats.WriteFailures != 0 {
			t.Fatal(stats)
		}
	})
}

// coordinatedMemory combines local leases and cache storage into one authority
// whose leased publication checks the proof before writing.
type coordinatedMemory struct {
	*cachememory.Backend
	leases *leasememory.Backend
}

func (b coordinatedMemory) LeaseAcquire(ctx context.Context, key lease.Key, owner lease.Owner, ttl time.Duration) (bool, error) {
	return b.leases.LeaseAcquire(ctx, key, owner, ttl)
}
func (b coordinatedMemory) LeaseRenew(ctx context.Context, key lease.Key, owner lease.Owner, ttl time.Duration) (bool, error) {
	return b.leases.LeaseRenew(ctx, key, owner, ttl)
}
func (b coordinatedMemory) LeaseRelease(ctx context.Context, key lease.Key, owner lease.Owner) (bool, error) {
	return b.leases.LeaseRelease(ctx, key, owner)
}
func (b coordinatedMemory) PutLeased(ctx context.Context, key cache.EntryKey, data []byte, ttl cache.TTL, proof lease.Proof) error {
	if err := cache.ValidateFillProof(ctx, key, proof); err != nil {
		return err
	}
	return b.Backend.Put(ctx, key, data, ttl)
}
func (b coordinatedMemory) PutTaggedLeased(ctx context.Context, key cache.TaggedKey, data []byte, ttl cache.TTL, proof lease.Proof) error {
	if err := cache.ValidateFillProof(ctx, key.FillKey(), proof); err != nil {
		return err
	}
	return b.Backend.PutTagged(ctx, key, data, ttl)
}

func TestCoordinatedWaiterObservesValueWhileLeaseIsHeld(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		clock := testkit.NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
		values, err := cachememory.New(cachememory.DefaultConfig(), clock)
		if err != nil {
			t.Fatal(err)
		}
		leases, err := leasememory.New(64)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = values.Close(context.Background()); _ = leases.Close() })
		authority := coordinatedMemory{Backend: values, leases: leases}
		coordination := cache.DefaultCoordinationConfig()
		coordination.Wait = time.Minute
		bind := func() cache.Cache[userKey, profile] {
			manager, err := lease.NewManager(authority, lease.DefaultConfig(namespace))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = manager.Close(context.Background()) })
			s, err := cache.NewCoordinatedStore(manager, cache.DefaultConfig(namespace), coordination)
			if err != nil {
				t.Fatal(err)
			}
			c, err := profiles.Bind(s)
			if err != nil {
				t.Fatal(err)
			}
			return c
		}
		first, second := bind(), bind()
		release := make(chan struct{})
		slow := asyncRemember(first, t.Context(), "key", func(context.Context) (profile, error) {
			<-release
			return profile{Name: "slow"}, nil
		})
		synctest.Wait()
		waiter := asyncRemember(second, t.Context(), "key", noLoad)
		synctest.Wait()
		// Another writer publishes while the slow loader still owns the fill lease.
		if err := second.Put(t.Context(), "key", profile{Name: "published"}, cache.Forever()); err != nil {
			t.Fatal(err)
		}
		if got := <-waiter; got.err != nil || got.value.Name != "published" {
			t.Fatal("waiter did not observe the published value", got)
		}
		close(release)
		if got := <-slow; got.err != nil {
			t.Fatal(got)
		}
	})
}

// With MaxFills exhausted a miss loads directly, but it stays in the fill chain:
// a loader that reloads its own key fails with Cycle instead of recursing.
func TestUncoalescedSelfRecursiveLoaderIsCycle(t *testing.T) {
	c := boundProfiles(t, func(config *cache.Config) { config.MaxFills = 1 })
	entered, release := make(chan struct{}), make(chan struct{})
	held := make(chan error, 1)
	go func() {
		_, err := c.Remember(t.Context(), "holder", cache.Forever(), func(context.Context) (profile, error) {
			close(entered)
			<-release
			return profile{Name: "holder"}, nil
		})
		held <- err
	}()
	<-entered
	defer func() {
		close(release)
		if err := <-held; err != nil {
			t.Error(err)
		}
	}()
	var depth atomic.Int32
	var recurse func(context.Context) (profile, error)
	recurse = func(ctx context.Context) (profile, error) {
		if depth.Add(1) > 3 {
			return profile{}, errors.New("unbounded recursion")
		}
		return c.Remember(ctx, "self", cache.Forever(), recurse)
	}
	if _, err := c.Remember(t.Context(), "self", cache.Forever(), recurse); !errors.Is(err, fault.Cycle) || depth.Load() != 1 {
		t.Fatal("direct self-recursive load was not a cycle", err, depth.Load())
	}
	// A different key inside a direct load is not a cycle.
	value, err := c.Remember(t.Context(), "outer", cache.Forever(), func(ctx context.Context) (profile, error) {
		return c.Remember(ctx, "inner", cache.Forever(), func(context.Context) (profile, error) { return profile{Name: "inner"}, nil })
	})
	if err != nil || value.Name != "inner" {
		t.Fatal(value, err)
	}
}
