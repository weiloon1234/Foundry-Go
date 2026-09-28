// Package cachetest owns behavior shared by Foundry's cache adapter acceptance.
package cachetest

import (
	"context"
	"errors"
	"math"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type Backend interface {
	cache.Backend
	cache.CounterBackend
}

// Run uses fresh owned addresses supplied by each adapter's test harness.
func Run(t *testing.T, setup func(*testing.T) (Backend, func(string) cache.EntryKey)) {
	t.Helper()
	t.Run("values-and-ownership", func(t *testing.T) {
		b, key := setup(t)
		k := key("value")
		if _, hit, err := b.Get(t.Context(), k); err != nil || hit {
			t.Fatal(hit, err)
		}
		for _, original := range [][]byte{nil, {}, []byte("value")} {
			expected := string(original)
			if err := b.Put(t.Context(), k, original, cache.For(time.Minute)); err != nil {
				t.Fatal(err)
			}
			if len(original) > 0 {
				original[0] = 'X'
			}
			got, hit, err := b.Get(t.Context(), k)
			if err != nil || !hit || string(got) != expected {
				t.Fatal(string(got), hit, err)
			}
			if len(got) > 0 {
				got[0] = 'Y'
			}
			again, _, err := b.Get(t.Context(), k)
			if err != nil || string(again) != expected {
				t.Fatal("read buffer retained", err)
			}
		}
		if _, hit, err := b.Get(t.Context(), key("other")); err != nil || hit {
			t.Fatal("key collision", hit, err)
		}
		if removed, err := b.Forget(t.Context(), k); err != nil || !removed {
			t.Fatal(removed, err)
		}
		if removed, err := b.Forget(t.Context(), k); err != nil || removed {
			t.Fatal(removed, err)
		}
	})
	t.Run("atomic-add", func(t *testing.T) {
		b, key := setup(t)
		k := key("add")
		var wins atomic.Int64
		var wg sync.WaitGroup
		for range 64 {
			wg.Go(func() {
				ok, err := b.Add(t.Context(), k, []byte("winner"), cache.For(time.Minute))
				if err != nil {
					t.Error(err)
				}
				if ok {
					wins.Add(1)
				}
			})
		}
		wg.Wait()
		if wins.Load() != 1 {
			t.Fatal(wins.Load())
		}
		got, hit, err := b.Get(t.Context(), k)
		if err != nil || !hit || string(got) != "winner" {
			t.Fatal(string(got), hit, err)
		}
	})
	t.Run("exact-counter", func(t *testing.T) {
		b, key := setup(t)
		k := key("counter")
		for _, initial := range []int64{0, 1<<53 + 1, math.MaxInt64, math.MinInt64} {
			if err := b.Put(t.Context(), k, []byte(strconv.FormatInt(initial, 10)), cache.For(time.Minute)); err != nil {
				t.Fatal(err)
			}
			got, err := b.Increment(t.Context(), k, 0, cache.Forever())
			if err != nil || got != initial {
				t.Fatal(got, initial, err)
			}
		}
		for _, initial := range []int64{math.MaxInt64, math.MinInt64} {
			if err := b.Put(t.Context(), k, []byte(strconv.FormatInt(initial, 10)), cache.For(time.Minute)); err != nil {
				t.Fatal(err)
			}
			delta := int64(1)
			if initial < 0 {
				delta = -1
			}
			if _, err := b.Increment(t.Context(), k, delta, cache.Forever()); !errors.Is(err, fault.Invalid) {
				t.Fatal(err)
			}
			value, _, err := b.Get(t.Context(), k)
			if err != nil || string(value) != strconv.FormatInt(initial, 10) {
				t.Fatal("overflow changed value", err)
			}
		}
		if _, err := b.Forget(t.Context(), k); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for range 64 {
			wg.Go(func() {
				if _, err := b.Increment(t.Context(), k, 1, cache.For(time.Minute)); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		value, err := b.Increment(t.Context(), k, 0, cache.Forever())
		if err != nil || value != 64 {
			t.Fatal(value, err)
		}
	})
	t.Run("corrupt-counter", func(t *testing.T) {
		b, key := setup(t)
		k := key("corrupt")
		for _, raw := range []string{"", "-0", "+1", "01", " 1", "1 ", "1.0", "1e2", "9223372036854775808", "-9223372036854775809"} {
			if err := b.Put(t.Context(), k, []byte(raw), cache.For(time.Minute)); err != nil {
				t.Fatal(err)
			}
			if _, err := b.Increment(t.Context(), k, 1, cache.Forever()); !errors.Is(err, fault.Invalid) {
				t.Fatal(raw, err)
			}
			value, _, err := b.Get(t.Context(), k)
			if err != nil || string(value) != raw {
				t.Fatal("corruption changed", raw, string(value), err)
			}
		}
	})
	t.Run("cancellation-and-invalid-ttl", func(t *testing.T) {
		b, key := setup(t)
		k := key("cancel")
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := b.Put(t.Context(), k, []byte("5"), cache.For(time.Minute)); err != nil {
			t.Fatal(err)
		}
		if err := b.Put(ctx, k, []byte("bad"), cache.Forever()); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, err := b.Add(ctx, k, []byte("bad"), cache.Forever()); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, err := b.Forget(ctx, k); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, _, err := b.Get(ctx, k); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, err := b.Increment(ctx, k, 1, cache.Forever()); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if err := b.Put(t.Context(), k, []byte("bad"), cache.TTL{}); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		value, _, err := b.Get(t.Context(), k)
		if err != nil || string(value) != "5" {
			t.Fatal(string(value), err)
		}
	})
}
