// Package leasetest holds the shared memory/Redis adapter acceptance contract.
package leasetest

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/lease"
)

type Fixture struct {
	Backend lease.Backend
	Key     func(string) lease.Key
	Expire  func(lease.Key)
}

func Run(t *testing.T, setup func(*testing.T) Fixture) {
	t.Helper()
	t.Run("ownership", func(t *testing.T) {
		f := setup(t)
		key := f.Key("owner")
		a, _ := lease.NewOwner()
		b, _ := lease.NewOwner()
		check(t, true, f.Backend.LeaseAcquire, t.Context(), key, a, time.Minute)
		check(t, false, f.Backend.LeaseAcquire, t.Context(), key, b, time.Minute)
		check(t, false, f.Backend.LeaseRenew, t.Context(), key, b, time.Hour)
		released, err := f.Backend.LeaseRelease(t.Context(), key, b)
		if err != nil || released {
			t.Fatal(released, err)
		}
		check(t, true, f.Backend.LeaseRenew, t.Context(), key, a, time.Minute)
		released, err = f.Backend.LeaseRelease(t.Context(), key, a)
		if err != nil || !released {
			t.Fatal(released, err)
		}
		check(t, true, f.Backend.LeaseAcquire, t.Context(), key, b, time.Minute)
		released, err = f.Backend.LeaseRelease(t.Context(), key, a)
		if err != nil || released {
			t.Fatal("stale release", released, err)
		}
		check(t, false, f.Backend.LeaseRenew, t.Context(), key, a, time.Hour)
	})
	t.Run("expiry", func(t *testing.T) {
		f := setup(t)
		key := f.Key("expiry")
		a, _ := lease.NewOwner()
		b, _ := lease.NewOwner()
		check(t, true, f.Backend.LeaseAcquire, t.Context(), key, a, lease.MinDuration)
		f.Expire(key)
		check(t, false, f.Backend.LeaseRenew, t.Context(), key, a, time.Minute)
		check(t, true, f.Backend.LeaseAcquire, t.Context(), key, b, time.Minute)
		ok, err := f.Backend.LeaseRelease(t.Context(), key, a)
		if err != nil || ok {
			t.Fatal("expired release", ok, err)
		}
	})
	t.Run("contention", func(t *testing.T) {
		f := setup(t)
		key := f.Key("contended")
		var won atomic.Int32
		var wg sync.WaitGroup
		for range 24 {
			wg.Go(func() {
				owner, _ := lease.NewOwner()
				ok, err := f.Backend.LeaseAcquire(t.Context(), key, owner, time.Minute)
				if err != nil {
					t.Error(err)
				}
				if ok {
					won.Add(1)
				}
			})
		}
		wg.Wait()
		if won.Load() != 1 {
			t.Fatal("winners", won.Load())
		}
	})
	t.Run("invalid-inputs-preserve-owner", func(t *testing.T) {
		f := setup(t)
		key := f.Key("invalid")
		owner, _ := lease.NewOwner()
		check(t, true, f.Backend.LeaseAcquire, t.Context(), key, owner, time.Minute)
		canceled, cancel := context.WithCancel(t.Context())
		cancel()
		for _, op := range []func(context.Context, lease.Key, lease.Owner, time.Duration) (bool, error){f.Backend.LeaseAcquire, f.Backend.LeaseRenew} {
			for _, bad := range []struct {
				ctx   context.Context
				key   lease.Key
				owner lease.Owner
				ttl   time.Duration
				want  error
			}{
				{nil, key, owner, time.Minute, fault.Invalid}, {canceled, key, owner, time.Minute, context.Canceled},
				{t.Context(), lease.Key{}, owner, time.Minute, fault.Invalid}, {t.Context(), key, lease.Owner{}, time.Minute, fault.Invalid},
				{t.Context(), key, owner, 0, fault.Invalid}, {t.Context(), key, owner, lease.MaxDuration + 1, fault.Invalid},
			} {
				ok, err := op(bad.ctx, bad.key, bad.owner, bad.ttl)
				if ok || !errors.Is(err, bad.want) {
					t.Fatal(ok, err)
				}
			}
		}
		if ok, err := f.Backend.LeaseRelease(canceled, key, owner); ok || !errors.Is(err, context.Canceled) {
			t.Fatal(ok, err)
		}
		check(t, true, f.Backend.LeaseRenew, t.Context(), key, owner, time.Minute)
	})
	t.Run("namespace-isolation", func(t *testing.T) {
		f := setup(t)
		a := f.Key("one")
		b := f.Key("two")
		owner, _ := lease.NewOwner()
		check(t, true, f.Backend.LeaseAcquire, t.Context(), a, owner, time.Minute)
		check(t, true, f.Backend.LeaseAcquire, t.Context(), b, owner, time.Minute)
	})
}
func check(t *testing.T, want bool, op func(context.Context, lease.Key, lease.Owner, time.Duration) (bool, error), ctx context.Context, key lease.Key, owner lease.Owner, ttl time.Duration) {
	t.Helper()
	got, err := op(ctx, key, owner, ttl)
	if err != nil || got != want {
		t.Fatal(got, err)
	}
}
