package cache_test

import (
	"context"
	"errors"
	"math"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
)

var counters = cache.DefineCounter("attempts", cache.StringKeys[userKey]())

func boundCounters(t *testing.T, s *cache.Store) cache.Counter[userKey] {
	t.Helper()
	c, err := counters.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCounterExactValuesCreationAndRetainedExpiry(t *testing.T) {
	s, _, clock := store(t, nil)
	c := boundCounters(t, s)
	if value, found, err := c.Get(t.Context(), "key"); err != nil || found || value != 0 {
		t.Fatal(value, found, err)
	}
	if value, err := c.Increment(t.Context(), "key", 9007199254740993, cache.For(time.Second)); err != nil || value != 9007199254740993 {
		t.Fatal("rounded above 2^53", value, err)
	}
	clock.Advance(500 * time.Millisecond)
	if value, err := c.Increment(t.Context(), "key", 1, cache.Forever()); err != nil || value != 9007199254740994 {
		t.Fatal(value, err)
	}
	if added, err := c.Add(t.Context(), "key", 0, cache.Forever()); err != nil || added {
		t.Fatal("Add replaced counter", added, err)
	}
	clock.Advance(500 * time.Millisecond)
	if _, found, err := c.Get(t.Context(), "key"); err != nil || found {
		t.Fatal("increment extended initial expiry", found, err)
	}
	if value, err := c.Decrement(t.Context(), "key", 2, cache.Forever()); err != nil || value != -2 {
		t.Fatal(value, err)
	}
	if value, err := c.Increment(t.Context(), "key", 0, cache.For(time.Nanosecond)); err != nil || value != -2 {
		t.Fatal(value, err)
	}
	clock.Advance(time.Hour)
	if value, found, err := c.Get(t.Context(), "key"); err != nil || !found || value != -2 {
		t.Fatal("persistent counter changed expiry", value, found, err)
	}
	if err := c.Put(t.Context(), "key", 0, cache.For(time.Second)); err != nil {
		t.Fatal(err)
	}
	if value, found, err := c.Get(t.Context(), "key"); err != nil || !found || value != 0 {
		t.Fatal("cached zero became miss", value, found, err)
	}
	clock.Advance(time.Second)
	if value, err := c.Increment(t.Context(), "key", math.MinInt64, cache.Forever()); err != nil || value != math.MinInt64 {
		t.Fatal(value, err)
	}
	if removed, err := c.Forget(t.Context(), "key"); err != nil || !removed {
		t.Fatal(removed, err)
	}
	if removed, err := c.Forget(t.Context(), "key"); err != nil || removed {
		t.Fatal(removed, err)
	}
}

func TestCounterOverflowRejectsWithoutChangingValueOrTTL(t *testing.T) {
	for _, test := range []struct {
		value, delta int64
		overflow     bool
	}{
		{math.MaxInt64, 1, true}, {math.MinInt64, -1, true},
		{-1, math.MinInt64, true}, {1, math.MaxInt64, true},
		{math.MinInt64, math.MaxInt64, false}, {math.MaxInt64, math.MinInt64, false},
	} {
		s, _, clock := store(t, nil)
		c := boundCounters(t, s)
		if err := c.Put(t.Context(), "key", test.value, cache.For(time.Second)); err != nil {
			t.Fatal(err)
		}
		value, err := c.Increment(t.Context(), "key", test.delta, cache.Forever())
		if test.overflow {
			if !errors.Is(err, fault.Invalid) || value != 0 {
				t.Fatal(test, value, err)
			}
			if got, found, err := c.Get(t.Context(), "key"); err != nil || !found || got != test.value {
				t.Fatal("overflow changed existing value", got, found, err)
			}
		} else if err != nil || value != -1 {
			t.Fatal(test, value, err)
		}
		clock.Advance(time.Second)
		if _, found, err := c.Get(t.Context(), "key"); err != nil || found {
			t.Fatal("overflow refreshed expiry", found, err)
		}
	}
}

func TestCounterConcurrentIncrementsAreAtomic(t *testing.T) {
	s, _, _ := store(t, nil)
	c := boundCounters(t, s)
	const workers, writes = 32, 64
	values := make(chan int64, workers*writes)
	errorsOut := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Go(func() {
			for range writes {
				value, err := c.Increment(t.Context(), "shared", 1, cache.Forever())
				if err != nil {
					errorsOut <- err
					return
				}
				values <- value
			}
		})
	}
	group.Wait()
	close(values)
	close(errorsOut)
	for err := range errorsOut {
		t.Fatal(err)
	}
	seen := make(map[int64]bool)
	for value := range values {
		if value < 1 || value > workers*writes || seen[value] {
			t.Fatal("lost or duplicated atomic increment", value)
		}
		seen[value] = true
	}
	if got, found, err := c.Get(t.Context(), "shared"); err != nil || !found || got != workers*writes || len(seen) != workers*writes {
		t.Fatal(got, found, err, len(seen))
	}
}

func TestCounterDeclarationsShareOwnershipAndRejectUnsupportedBackends(t *testing.T) {
	s, backend, _ := store(t, func(c *cache.Config) { c.MaxDeclarations = 1 })
	_ = boundCounters(t, s)
	copy := counters
	if _, err := copy.Bind(s); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.DefineCounter("attempts", cache.StringKeys[userKey]()).Bind(s); !errors.Is(err, fault.Duplicate) {
		t.Fatal(err)
	}
	if _, err := cache.Define("attempts", cache.StringKeys[userKey](), cache.JSON[int64]()).Bind(s); !errors.Is(err, fault.Duplicate) {
		t.Fatal("ordinary cache reused counter identity", err)
	}
	if _, err := cache.DefineCounter("other", cache.StringKeys[userKey]()).Bind(s); !errors.Is(err, fault.Invalid) {
		t.Fatal("declaration bound ignored", err)
	}
	var zero cache.CounterDeclaration[userKey]
	if zero.Name() != "" || zero.Validate() == nil {
		t.Fatal("zero counter declaration accepted")
	}
	if _, err := zero.Bind(s); err == nil {
		t.Fatal("zero counter declaration bound")
	}
	if _, err := counters.Bind(nil); err == nil {
		t.Fatal("nil store accepted")
	}
	if _, err := cache.DefineCounter("invalid:name", cache.StringKeys[userKey]()).Bind(s); err == nil {
		t.Fatal("invalid family accepted")
	}
	if _, err := cache.DefineCounter("valid", cache.KeyCodec[userKey]{}).Bind(s); err == nil {
		t.Fatal("invalid codec accepted")
	}

	// A wrapper exposing only the basic capability must not emulate increments
	// using a non-atomic read/write pair or reserve a rejected declaration name.
	unsupported, err := cache.NewStore(struct{ cache.Backend }{backend}, cache.DefaultConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := counters.Bind(unsupported); !errors.Is(err, fault.Invalid) {
		t.Fatal("unsupported counter bound", err)
	}
	if _, err := cache.Define("attempts", cache.StringKeys[userKey](), cache.JSON[int64]()).Bind(unsupported); err != nil {
		t.Fatal("failed bind claimed a family", err)
	}
	small, _, _ := store(t, func(c *cache.Config) { c.MaxValueBytes = 19 })
	if _, err := counters.Bind(small); !errors.Is(err, fault.Invalid) {
		t.Fatal("partial integer range accepted", err)
	}
}

func TestCounterInvalidOperationsAndClose(t *testing.T) {
	s, backend, _ := store(t, nil)
	c := boundCounters(t, s)
	if _, err := c.Increment(t.Context(), "key", 1, cache.TTL{}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := c.Decrement(t.Context(), "key", -1, cache.Forever()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, found, err := c.Get(t.Context(), "key"); err != nil || found {
		t.Fatal("invalid mutation stored value", found, err)
	}
	if _, err := c.Increment(nil, "key", 1, cache.Forever()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := c.Increment(t.Context(), "bad\nkey", 1, cache.Forever()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Increment(ctx, "key", 1, cache.Forever()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	var zero cache.Counter[userKey]
	if _, err := zero.Increment(t.Context(), "key", 1, cache.Forever()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if err := backend.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Increment(t.Context(), "key", 1, cache.Forever()); !errors.Is(err, fault.Closed) {
		t.Fatal(err)
	}
}

type failingCounter struct {
	cache.Backend
	increment func(context.Context, cache.EntryKey, int64, cache.TTL) (int64, error)
}

func (b failingCounter) Increment(ctx context.Context, key cache.EntryKey, delta int64, ttl cache.TTL) (int64, error) {
	return b.increment(ctx, key, delta, ttl)
}
func TestCounterBackendFailuresRemainErrorsAndAreNotRetried(t *testing.T) {
	cause := errors.New("private counter detail")
	for _, test := range []struct {
		name   string
		fail   func(context.Context) (int64, error)
		target error
	}{
		{"error", func(context.Context) (int64, error) { return 123, cause }, cause},
		{"panic", func(context.Context) (int64, error) { panic("private counter detail") }, fault.Panicked},
		{"goexit", func(context.Context) (int64, error) { runtime.Goexit(); return 0, nil }, fault.Panicked},
		{"timeout", func(ctx context.Context) (int64, error) { <-ctx.Done(); return 123, nil }, context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, backend, _ := store(t, nil)
			calls := 0
			adapter := failingCounter{Backend: backend, increment: func(ctx context.Context, _ cache.EntryKey, _ int64, _ cache.TTL) (int64, error) {
				calls++
				return test.fail(ctx)
			}}
			config := cache.DefaultConfig(namespace)
			config.Timeout = 10 * time.Millisecond
			s, err := cache.NewStore(adapter, config)
			if err != nil {
				t.Fatal(err)
			}
			c := boundCounters(t, s)
			value, err := c.Increment(t.Context(), "key", 1, cache.Forever())
			if value != 0 || !errors.Is(err, test.target) || strings.Contains(err.Error(), "private") || calls != 1 {
				t.Fatal(value, err, calls)
			}
		})
	}
}
