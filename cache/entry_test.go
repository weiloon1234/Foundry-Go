package cache_test

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cache/memory"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type observedEntryBackend struct {
	*memory.Backend
	resolves    int
	beforeBatch func(context.Context, []cache.TaggedKey) error
}

func (b *observedEntryBackend) ResolveTags(ctx context.Context, keys []cache.EntryKey) ([]cache.TagVersion, error) {
	b.resolves++
	return b.Backend.ResolveTags(ctx, keys)
}
func (b *observedEntryBackend) ForgetManyTagged(ctx context.Context, keys []cache.TaggedKey) (uint64, error) {
	if b.beforeBatch != nil {
		if err := b.beforeBatch(ctx, keys); err != nil {
			return 0, err
		}
	}
	return b.Backend.ForgetManyTagged(ctx, keys)
}
func TestTypedEntryOperationsSkipValueCodecsAndDeduplicate(t *testing.T) {
	s, _, _ := store(t, func(c *cache.Config) { c.MaxBatchEntries = 3 })
	encodes, decodes := 0, 0
	codec := cache.NewCodec(func(int) ([]byte, error) { encodes++; return []byte("value"), nil }, func([]byte) (int, error) { decodes++; return 0, errors.New("decoder must not run") })
	c, err := cache.Define("entry-operations", cache.StringKeys[userKey](), codec).Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []userKey{"one", "two"} {
		if err := c.Put(t.Context(), key, 7, cache.Forever()); err != nil {
			t.Fatal(err)
		}
	}
	encodes = 0
	if hit, err := c.Exists(t.Context(), "one"); err != nil || !hit {
		t.Fatal(hit, err)
	}
	if changed, err := c.Expire(t.Context(), "one", cache.For(time.Minute)); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if count, err := c.ForgetMany(t.Context(), "one", "two", "one", "two"); count != 0 || !errors.Is(err, fault.Invalid) {
		t.Fatal("bound applied after deduplication", count, err)
	}
	if count, err := c.ForgetMany(t.Context(), "two", "one", "two"); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	if encodes != 0 || decodes != 0 {
		t.Fatal("entry operation ran payload codec", encodes, decodes)
	}
	if hit, err := c.Exists(t.Context(), "one"); err != nil || hit {
		t.Fatal(hit, err)
	}
	if count, err := c.ForgetMany(t.Context()); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	counters := boundCounters(t, s)
	if err := counters.Put(t.Context(), "counter", 41, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if ok, err := counters.Expire(t.Context(), "counter", cache.Forever()); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if ok, err := counters.Exists(t.Context(), "counter"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if count, err := counters.ForgetMany(t.Context(), "counter", "counter"); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
func TestTypedBatchEncodesAllKeysBeforeResolvingOneSnapshot(t *testing.T) {
	_, memory, _ := store(t, nil)
	backend := &observedEntryBackend{Backend: memory}
	s, err := cache.NewStore(backend, cache.DefaultConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("private key failure")
	keys := cache.NewKeyCodec(func(key string) (string, error) {
		if key == "bad" {
			return "", cause
		}
		return key, nil
	})
	c, err := cache.Define("atomic-batch", keys, cache.JSON[int]()).Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"one", "two"} {
		if err := c.Put(t.Context(), key, 7, cache.Forever()); err != nil {
			t.Fatal(err)
		}
	}
	backend.resolves = 0
	if count, err := c.ForgetMany(t.Context(), "one", "bad"); count != 0 || !errors.Is(err, cause) || strings.Contains(err.Error(), "private") {
		t.Fatal(count, err)
	}
	if backend.resolves != 0 {
		t.Fatal("metadata initialized before later key validation", backend.resolves)
	}
	for _, key := range []string{"one", "two"} {
		if value, hit, err := c.Get(t.Context(), key); err != nil || !hit || value != 7 {
			t.Fatal(value, hit, err)
		}
	}
	backend.resolves = 0
	if count, err := c.ForgetMany(t.Context(), "one", "two"); err != nil || count != 2 || backend.resolves != 1 {
		t.Fatal("batch did not capture one snapshot", count, err, backend.resolves)
	}
}
func TestTypedBatchStaleSnapshotCannotDeleteNewerValue(t *testing.T) {
	_, memory, _ := store(t, nil)
	backend := &observedEntryBackend{Backend: memory}
	s, err := cache.NewStore(backend, cache.DefaultConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	c, err := cache.Define("rotating-batch", cache.StringKeys[string](), cache.JSON[string]()).Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Put(t.Context(), "key", "old", cache.Forever()); err != nil {
		t.Fatal(err)
	}
	backend.beforeBatch = func(ctx context.Context, _ []cache.TaggedKey) error {
		if err := s.Invalidate(ctx); err != nil {
			return err
		}
		return c.Put(ctx, "key", "new", cache.Forever())
	}
	if count, err := c.ForgetMany(t.Context(), "key"); count != 0 || !errors.Is(err, fault.Conflict) {
		t.Fatal(count, err)
	}
	if value, hit, err := c.Get(t.Context(), "key"); err != nil || !hit || value != "new" {
		t.Fatal("new publication was removed", value, hit, err)
	}
}
func TestTypedEntryInvalidHandlesCapabilitiesAndCancellation(t *testing.T) {
	_, backend, _ := store(t, nil)
	s, err := cache.NewStore(struct{ cache.Backend }{backend}, cache.DefaultConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	c, err := profiles.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Exists(t.Context(), "key"); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := c.Expire(t.Context(), "key", cache.Forever()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := c.ForgetMany(t.Context(), "key"); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	var zero cache.Cache[userKey, profile]
	if _, err := zero.ForgetMany(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := c.ForgetMany(nil); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.ForgetMany(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := c.Expire(t.Context(), "key", cache.TTL{}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
func TestTypedBatchCallbackFailuresAndLateExitRemainOwned(t *testing.T) {
	for _, mode := range []string{"panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			s, backend, _ := store(t, nil)
			c, err := cache.Define("failed-batch", cache.NewKeyCodec(func(key string) (string, error) {
				if key == "bad" {
					if mode == "panic" {
						panic("private key")
					}
					runtime.Goexit()
				}
				return key, nil
			}), cache.JSON[string]()).Bind(s)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.ForgetMany(t.Context(), "good", "bad"); !errors.Is(err, fault.Panicked) {
				t.Fatal(err)
			}
			if backend.Stats().Entries != 0 {
				t.Fatal("failed key batch touched storage")
			}
		})
	}
	synctest.Test(t, func(t *testing.T) {
		s, backend, _ := store(t, func(c *cache.Config) { c.Timeout = time.Second })
		entered := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		c, err := cache.Define("slow-batch", cache.NewKeyCodec(func(key string) (string, error) {
			if key == "slow" {
				close(entered)
				<-release
			}
			return key, nil
		}), cache.JSON[string]()).Bind(s)
		if err != nil {
			t.Fatal(err)
		}
		go func() { _, err := c.ForgetMany(t.Context(), "good", "slow"); done <- err }()
		<-entered
		time.Sleep(2 * time.Second)
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("live key callback was detached")
		default:
		}
		close(release)
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		if backend.Stats().Entries != 0 {
			t.Fatal("canceled key batch touched storage")
		}
	})
}
