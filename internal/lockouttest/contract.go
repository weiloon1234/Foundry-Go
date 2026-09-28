// Package lockouttest owns behavioral acceptance shared by lockout authorities.
package lockouttest

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func Generation(t *testing.T) lockout.Generation {
	t.Helper()
	g, err := lockout.NewGeneration()
	if err != nil {
		t.Fatal(err)
	}
	return g
}
func Begin(t *testing.T, b lockout.Backend, k lockout.Key, p lockout.Policy) lockout.Snapshot {
	t.Helper()
	a, err := b.LockoutBegin(t.Context(), k, p, Generation(t))
	if err != nil || a.Decision.Status != lockout.StatusAllowed {
		t.Fatal("admission", a, err)
	}
	if err := a.Validate(p); err != nil {
		t.Fatal(err)
	}
	return a.Snapshot
}
func Finish(t *testing.T, b lockout.Backend, k lockout.Key, p lockout.Policy, s lockout.Snapshot, o lockout.Outcome) lockout.Decision {
	t.Helper()
	d, err := b.LockoutFinish(t.Context(), k, p, s, o)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(p); err != nil {
		t.Fatal(err)
	}
	return d
}
func Run(t *testing.T, factory func(*testing.T) (lockout.Backend, func(string) lockout.Key)) {
	t.Helper()
	t.Run("threshold-reset-and-generation", func(t *testing.T) {
		b, key := factory(t)
		k := key("threshold")
		p := lockout.DefaultPolicy()
		p.MaxFailures = 3
		for i := 0; i < 3; i++ {
			d := Finish(t, b, k, p, Begin(t, b, k, p), lockout.Failed)
			if (d.Status == lockout.StatusLocked) != (i == 2) || d.Triggered != (i == 2) {
				t.Fatal("wrong threshold", d)
			}
		}
		a, err := b.LockoutBegin(t.Context(), k, p, Generation(t))
		if err != nil || a.Decision.Status != lockout.StatusLocked || a.Snapshot != (lockout.Snapshot{}) {
			t.Fatal("locked admission", a, err)
		}
		if err := a.Validate(p); err != nil {
			t.Fatal(err)
		}
		Begin(t, b, key("other-account"), p)
		if changed, err := b.LockoutReset(t.Context(), k, p); err != nil || !changed {
			t.Fatal("reset", err)
		}
		old := Begin(t, b, k, p)
		if _, err := b.LockoutReset(t.Context(), k, p); err != nil {
			t.Fatal(err)
		}
		current := Begin(t, b, k, p)
		if current.Generation == old.Generation {
			t.Fatal("reset reused generation")
		}
		for _, outcome := range []lockout.Outcome{lockout.Failed, lockout.Succeeded} {
			if d := Finish(t, b, k, p, old, outcome); d.Status != lockout.StatusExpired {
				t.Fatal("old window affected new state", d)
			}
		}
		if d := Finish(t, b, k, p, current, lockout.Failed); d.Status != lockout.StatusAllowed {
			t.Fatal("stale failure consumed new window", d)
		}
	})
	t.Run("success-does-not-clear-newer-failures", func(t *testing.T) {
		b, key := factory(t)
		k := key("late-success")
		p := lockout.DefaultPolicy()
		p.MaxFailures = 3
		Finish(t, b, k, p, Begin(t, b, k, p), lockout.Failed)
		earlySuccess := Begin(t, b, k, p)
		Finish(t, b, k, p, Begin(t, b, k, p), lockout.Failed)
		if d := Finish(t, b, k, p, earlySuccess, lockout.Succeeded); d.Status != lockout.StatusAllowed {
			t.Fatal(d)
		}
		if d := Finish(t, b, k, p, Begin(t, b, k, p), lockout.Failed); d.Status != lockout.StatusLocked {
			t.Fatal("late success erased newer failure", d)
		}
	})
	t.Run("success-resets-observed-failures", func(t *testing.T) {
		b, key := factory(t)
		k := key("current-success")
		p := lockout.DefaultPolicy()
		p.MaxFailures = 3
		for range 2 {
			Finish(t, b, k, p, Begin(t, b, k, p), lockout.Failed)
		}
		if d := Finish(t, b, k, p, Begin(t, b, k, p), lockout.Succeeded); d.Status != lockout.StatusAllowed {
			t.Fatal(d)
		}
		for range 2 {
			if d := Finish(t, b, k, p, Begin(t, b, k, p), lockout.Failed); d.Status != lockout.StatusAllowed {
				t.Fatal("old failures retained", d)
			}
		}
		if d := Finish(t, b, k, p, Begin(t, b, k, p), lockout.Failed); d.Status != lockout.StatusLocked {
			t.Fatal(d)
		}
	})
	t.Run("parallel-failures-and-late-success", func(t *testing.T) {
		b, key := factory(t)
		k := key("parallel")
		p := lockout.DefaultPolicy()
		p.MaxFailures = 7
		success := Begin(t, b, k, p)
		snapshots := make([]lockout.Snapshot, 32)
		for i := range snapshots {
			snapshots[i] = Begin(t, b, k, p)
		}
		var triggered, allowed atomic.Int32
		var wg sync.WaitGroup
		for _, s := range snapshots {
			wg.Go(func() {
				d, err := b.LockoutFinish(t.Context(), k, p, s, lockout.Failed)
				if err != nil {
					t.Error(err)
					return
				}
				if d.Triggered {
					triggered.Add(1)
				}
				if d.Status == lockout.StatusAllowed {
					allowed.Add(1)
				}
			})
		}
		wg.Wait()
		if triggered.Load() != 1 || allowed.Load() != 6 {
			t.Fatal("non-atomic threshold", triggered.Load(), allowed.Load())
		}
		if d := Finish(t, b, k, p, success, lockout.Succeeded); d.Status != lockout.StatusLocked || d.Triggered {
			t.Fatal("successful in-flight attempt bypassed lock", d)
		}
	})
	t.Run("policy-conflicts-and-invalid-inputs", func(t *testing.T) {
		b, key := factory(t)
		k := key("policy")
		p := lockout.DefaultPolicy()
		s := Begin(t, b, k, p)
		changed := p
		changed.MaxFailures++
		if _, err := b.LockoutBegin(t.Context(), k, changed, Generation(t)); !errors.Is(err, fault.Conflict) {
			t.Fatal(err)
		}
		if _, err := b.LockoutFinish(t.Context(), k, changed, s, lockout.Failed); !errors.Is(err, fault.Conflict) {
			t.Fatal(err)
		}
		if _, err := b.LockoutReset(t.Context(), k, changed); !errors.Is(err, fault.Conflict) {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := b.LockoutBegin(ctx, k, p, Generation(t)); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, err := b.LockoutBegin(nil, k, p, Generation(t)); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		if _, err := b.LockoutBegin(t.Context(), lockout.Key{}, p, Generation(t)); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		if _, err := b.LockoutBegin(t.Context(), k, p, lockout.Generation{}); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		if _, err := b.LockoutFinish(t.Context(), k, p, s, 0); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		if _, err := b.LockoutFinish(t.Context(), k, p, lockout.Snapshot{}, lockout.Succeeded); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		future := s
		future.Revision++
		if _, err := b.LockoutFinish(t.Context(), k, p, future, lockout.Failed); !errors.Is(err, fault.Invalid) {
			t.Fatal("future revision accepted", err)
		}
		if d := Finish(t, b, k, p, s, lockout.Succeeded); d.Status != lockout.StatusAllowed {
			t.Fatal(d)
		}
	})
}
