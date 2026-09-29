// Package ratelimittest owns behavioral acceptance shared by local and Redis authorities.
package ratelimittest

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func Run(t *testing.T, factory func(*testing.T) (ratelimit.Backend, func(string) ratelimit.Key)) {
	t.Helper()
	t.Run("weighted-denial-preserves-capacity", func(t *testing.T) {
		b, key := factory(t)
		k := key("weighted")
		limit := ratelimit.PerHour(5)
		for _, step := range []struct {
			cost      uint32
			allowed   bool
			remaining uint32
		}{{3, true, 2}, {3, false, 2}, {2, true, 0}, {1, false, 0}} {
			d, err := b.RateLimit(t.Context(), k, limit, step.cost)
			if err != nil || d.Allowed != step.allowed || d.Remaining != step.remaining {
				t.Fatal(d, err, step)
			}
			if err := d.Validate(limit, step.cost); err != nil {
				t.Fatal(err)
			}
		}
		if d, err := b.RateLimit(t.Context(), key("independent"), limit, 5); err != nil || !d.Allowed {
			t.Fatal(d, err)
		}
	})
	t.Run("exact-maximum-and-live-policy", func(t *testing.T) {
		b, key := factory(t)
		k := key("maximum")
		limit := ratelimit.PerHour(math.MaxUint32)
		if d, err := b.RateLimit(t.Context(), k, limit, math.MaxUint32-1); err != nil || !d.Allowed || d.Remaining != 1 {
			t.Fatal(d, err)
		}
		// A changed policy converts the live bucket instead of failing; admitted
		// usage still counts (capped), and a denial persists no conversion.
		if d, err := b.RateLimit(t.Context(), k, ratelimit.PerHour(2), 1); err != nil || d.Allowed || d.Remaining != 0 || d.RetryAfter != d.ResetAfter {
			t.Fatal(d, err)
		}
		if d, err := b.RateLimit(t.Context(), k, limit, math.MaxUint32); err != nil || d.Allowed || d.Remaining != 1 {
			t.Fatal(d, err)
		}
		if d, err := b.RateLimit(t.Context(), k, limit, 1); err != nil || !d.Allowed || d.Remaining != 0 {
			t.Fatal(d, err)
		}
		converted := ratelimit.PerMinute(math.MaxUint32)
		d, err := b.RateLimit(t.Context(), k, converted, 1)
		if err != nil || d.Allowed || d.Remaining != 0 || d.ResetAfter > time.Minute {
			t.Fatal("converted bucket reopened quota or kept the old window", d, err)
		}
		if err := d.Validate(converted, 1); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("peek-and-clear", func(t *testing.T) {
		b, key := factory(t)
		inspect, ok := b.(ratelimit.InspectBackend)
		if !ok {
			t.Skip("backend does not implement inspection")
		}
		k := key("inspected")
		limit := ratelimit.PerHour(5)
		d, err := inspect.PeekRateLimit(t.Context(), k, limit, 5)
		if err != nil || !d.Allowed || d.Remaining != 5 || d.RetryAfter != 0 || d.ValidatePeek(limit, 5) != nil {
			t.Fatal("empty bucket", d, err)
		}
		if d, err := b.RateLimit(t.Context(), k, limit, 3); err != nil || !d.Allowed || d.Remaining != 2 {
			t.Fatal(d, err)
		}
		for range 2 {
			d, err := inspect.PeekRateLimit(t.Context(), k, limit, 2)
			if err != nil || !d.Allowed || d.Remaining != 2 || d.ValidatePeek(limit, 2) != nil {
				t.Fatal("peek consumed or misreported capacity", d, err)
			}
		}
		d, err = inspect.PeekRateLimit(t.Context(), k, limit, 3)
		if err != nil || d.Allowed || d.Remaining != 2 || d.RetryAfter != d.ResetAfter || d.ValidatePeek(limit, 3) != nil {
			t.Fatal(d, err)
		}
		if d, err := b.RateLimit(t.Context(), k, limit, 2); err != nil || !d.Allowed || d.Remaining != 0 {
			t.Fatal("peek changed usage", d, err)
		}
		if cleared, err := inspect.ClearRateLimit(t.Context(), k); err != nil || !cleared {
			t.Fatal(cleared, err)
		}
		if cleared, err := inspect.ClearRateLimit(t.Context(), k); err != nil || cleared {
			t.Fatal("clear of an absent bucket", cleared, err)
		}
		if d, err := b.RateLimit(t.Context(), k, limit, 5); err != nil || !d.Allowed || d.Remaining != 0 {
			t.Fatal("cleared bucket kept usage", d, err)
		}
		if _, err := inspect.PeekRateLimit(t.Context(), k, limit, 0); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		if _, err := inspect.ClearRateLimit(nil, k); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		if _, err := inspect.ClearRateLimit(t.Context(), ratelimit.Key{}); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	})
	t.Run("atomic-contention", func(t *testing.T) {
		b, key := factory(t)
		k := key("contended")
		limit := ratelimit.PerHour(19)
		var admitted atomic.Uint32
		var wg sync.WaitGroup
		for range 64 {
			wg.Go(func() {
				d, err := b.RateLimit(t.Context(), k, limit, 1)
				if err != nil {
					t.Error(err)
					return
				}
				if d.Allowed {
					admitted.Add(1)
				}
			})
		}
		wg.Wait()
		if admitted.Load() != 19 {
			t.Fatal(admitted.Load())
		}
	})
	t.Run("invalid-and-canceled-inputs", func(t *testing.T) {
		b, key := factory(t)
		k := key("validation")
		limit := ratelimit.PerHour(2)
		for _, cost := range []uint32{0, 3} {
			if d, err := b.RateLimit(t.Context(), k, limit, cost); !errors.Is(err, fault.Invalid) || d != (ratelimit.Decision{}) {
				t.Fatal(d, err)
			}
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if d, err := b.RateLimit(ctx, k, limit, 1); !errors.Is(err, context.Canceled) || d != (ratelimit.Decision{}) {
			t.Fatal(d, err)
		}
		if _, err := b.RateLimit(nil, k, limit, 1); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		if _, err := b.RateLimit(t.Context(), ratelimit.Key{}, limit, 1); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		if d, err := b.RateLimit(t.Context(), k, limit, 2); err != nil || !d.Allowed {
			t.Fatal(d, err)
		}
	})
}
