package memory_test

import (
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/auth/lockout/memory"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/lockouttest"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func fixture(t *testing.T, capacity int) (*memory.Backend, *testkit.Clock, func(string) lockout.Key) {
	t.Helper()
	clock := testkit.NewClock(time.Unix(1000, 0))
	b, err := memory.New(capacity, clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	return b, clock, func(logical string) lockout.Key {
		k, err := lockout.NewKey(keyspace.Namespace{Application: "test", Environment: "lockout"}, "password", logical)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
}
func TestSharedContract(t *testing.T) {
	lockouttest.Run(t, func(t *testing.T) (lockout.Backend, func(string) lockout.Key) {
		b, _, key := fixture(t, 100)
		return b, key
	})
}
func TestWindowAndLockExpiryNeverSlide(t *testing.T) {
	b, clock, key := fixture(t, 10)
	p := lockout.Policy{MaxFailures: 2, Window: time.Minute, LockFor: 3 * time.Second}
	k := key("member")
	first := lockouttest.Begin(t, b, k, p)
	clock.Advance(time.Minute)
	if d := lockouttest.Finish(t, b, k, p, first, lockout.Succeeded); d.Status != lockout.StatusExpired {
		t.Fatal("expired attempt authenticated", d)
	}
	second := lockouttest.Begin(t, b, k, p)
	if first.Generation == second.Generation {
		t.Fatal("expired generation reused")
	}
	lockouttest.Finish(t, b, k, p, second, lockout.Failed)
	locked := lockouttest.Finish(t, b, k, p, lockouttest.Begin(t, b, k, p), lockout.Failed)
	if locked.RetryAfter != 3*time.Second {
		t.Fatal(locked)
	}
	clock.Advance(2 * time.Second)
	denied, err := b.LockoutBegin(t.Context(), k, p, lockouttest.Generation(t))
	if err != nil || denied.Decision.RetryAfter != time.Second {
		t.Fatal(denied, err)
	}
	clock.Advance(time.Second)
	fresh := lockouttest.Begin(t, b, k, p)
	if fresh.Generation == second.Generation {
		t.Fatal("unlock retained threshold window")
	}
	if d := lockouttest.Finish(t, b, k, p, fresh, lockout.Failed); d.Status != lockout.StatusAllowed {
		t.Fatal(d)
	}
}
func TestCapacityClockAndCloseFailClosed(t *testing.T) {
	b, clock, key := fixture(t, 1)
	p := lockout.DefaultPolicy()
	k := key("first")
	first := lockouttest.Begin(t, b, k, p)
	if _, err := b.LockoutBegin(t.Context(), key("second"), p, lockouttest.Generation(t)); !errors.Is(err, fault.Conflict) {
		t.Fatal("evicted live entry", err)
	}
	clock.Advance(-time.Millisecond)
	if _, err := b.LockoutBegin(t.Context(), k, p, lockouttest.Generation(t)); !errors.Is(err, fault.Conflict) {
		t.Fatal("backward clock accepted", err)
	}
	clock.Advance(p.Window + time.Millisecond)
	lockouttest.Begin(t, b, key("second"), p)
	if d := lockouttest.Finish(t, b, k, p, first, lockout.Failed); d.Status != lockout.StatusExpired {
		t.Fatal(d)
	}
	b.Close()
	if _, err := b.LockoutBegin(t.Context(), k, p, lockouttest.Generation(t)); !errors.Is(err, fault.Closed) {
		t.Fatal("closed backend accepted", err)
	}
}
