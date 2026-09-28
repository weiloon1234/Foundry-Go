package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/lockouttest"
)

func lockoutFixture(t *testing.T) (*Client, func(string) lockout.Key) {
	t.Helper()
	c, namespace, track := integrationAddresses(t, nil)
	return c, func(logical string) lockout.Key {
		k, err := lockout.NewKey(namespace, "password", logical)
		if err != nil {
			t.Fatal(err)
		}
		track(k.String())
		return k
	}
}
func TestLockoutSharedContract(t *testing.T) {
	lockouttest.Run(t, func(t *testing.T) (lockout.Backend, func(string) lockout.Key) { return lockoutFixture(t) })
}
func TestLockoutTwoClientsShareCountsAndLateSuccessRejection(t *testing.T) {
	c, key := lockoutFixture(t)
	other, err := Open(t.Context(), integrationConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close(context.Background()) })
	k := key("two-clients")
	p := lockout.DefaultPolicy()
	p.MaxFailures = 2
	success := lockouttest.Begin(t, c, k, p)
	lockouttest.Finish(t, other, k, p, lockouttest.Begin(t, other, k, p), lockout.Failed)
	d := lockouttest.Finish(t, c, k, p, lockouttest.Begin(t, c, k, p), lockout.Failed)
	if d.Status != lockout.StatusLocked || !d.Triggered {
		t.Fatal(d)
	}
	if d := lockouttest.Finish(t, other, k, p, success, lockout.Succeeded); d.Status != lockout.StatusLocked {
		t.Fatal("second client bypassed lock", d)
	}
}
func TestLockoutRedisClockExpiryAndDenialsDoNotRenew(t *testing.T) {
	c, key := lockoutFixture(t)
	now := c.raw.Time(t.Context()).Val().UnixMilli()
	start := now + time.Hour.Milliseconds()
	clock := &rateClockHook{script: authLockoutScript}
	clock.millis.Store(start)
	c.raw.AddHook(clock)
	p := lockout.Policy{MaxFailures: 2, Window: time.Minute, LockFor: 3 * time.Second}
	k := key("expiry")
	old := lockouttest.Begin(t, c, k, p)
	lockouttest.Finish(t, c, k, p, old, lockout.Failed)
	clock.millis.Store(start + 500)
	d := lockouttest.Finish(t, c, k, p, lockouttest.Begin(t, c, k, p), lockout.Failed)
	if d.RetryAfter != p.LockFor {
		t.Fatal(d)
	}
	wire := c.raw.Get(t.Context(), k.String()).Val()
	expiry := c.raw.PExpireTime(t.Context(), k.String()).Val().Milliseconds()
	if expiry != start+3500 {
		t.Fatal("wrong server expiry", expiry)
	}
	clock.millis.Store(start + 2500)
	a, err := c.LockoutBegin(t.Context(), k, p, lockouttest.Generation(t))
	if err != nil || a.Decision.RetryAfter != time.Second {
		t.Fatal(a, err)
	}
	if c.raw.Get(t.Context(), k.String()).Val() != wire || c.raw.PExpireTime(t.Context(), k.String()).Val().Milliseconds() != expiry {
		t.Fatal("denial renewed lockout")
	}
	clock.millis.Store(start + 499)
	if _, err := c.LockoutBegin(t.Context(), k, p, lockouttest.Generation(t)); !errors.Is(err, fault.Conflict) {
		t.Fatal("backward clock accepted", err)
	}
	clock.millis.Store(start + 3500)
	fresh := lockouttest.Begin(t, c, k, p)
	if fresh.Generation == old.Generation {
		t.Fatal("unlock reused generation")
	}
	clock.millis.Store(start + 3500 + p.Window.Milliseconds())
	if d := lockouttest.Finish(t, c, k, p, fresh, lockout.Succeeded); d.Status != lockout.StatusExpired {
		t.Fatal("late success admitted", d)
	}
	changed := p
	changed.MaxFailures++
	lockouttest.Begin(t, c, k, changed)
}
func TestLockoutCorruptStateFailsWithoutChangingStoredValue(t *testing.T) {
	c, key := lockoutFixture(t)
	p := lockout.DefaultPolicy()
	for i, wire := range []string{"private-data", strings.Repeat("x", lockoutMetadataBytes+1), "1:0:1000:1000:a:0:0:0:0:0"} {
		k := key(fmt.Sprint("bad-", i))
		if err := c.raw.Set(t.Context(), k.String(), wire, time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
		if _, err := c.LockoutBegin(t.Context(), k, p, lockouttest.Generation(t)); !errors.Is(err, fault.Invalid) || strings.Contains(err.Error(), "private") {
			t.Fatal(err)
		}
		if c.raw.Get(t.Context(), k.String()).Val() != wire {
			t.Fatal("corruption overwritten")
		}
	}
	for _, mode := range []string{"list", "missing-expiry", "revision", "generation", "counter"} {
		k := key(mode)
		lockouttest.Begin(t, c, k, p)
		if mode == "list" {
			c.raw.Del(t.Context(), k.String())
			c.raw.LPush(t.Context(), k.String(), "private")
		} else if mode == "missing-expiry" {
			c.raw.Persist(t.Context(), k.String())
		} else {
			parts := strings.Split(c.raw.Get(t.Context(), k.String()).Val(), ":")
			switch mode {
			case "revision":
				parts[8] = "01"
			case "generation":
				parts[4] = strings.Repeat("0", 64)
			case "counter":
				parts[7] = "99999"
			}
			c.raw.SetArgs(t.Context(), k.String(), strings.Join(parts, ":"), driver.SetArgs{KeepTTL: true})
		}
		beforeType := c.raw.Type(t.Context(), k.String()).Val()
		if _, err := c.LockoutReset(t.Context(), k, p); !errors.Is(err, fault.Invalid) {
			t.Fatal("corrupt reset accepted", mode, err)
		}
		if c.raw.Type(t.Context(), k.String()).Val() != beforeType {
			t.Fatal("corrupt key deleted")
		}
	}
}

type lostLockoutFinish struct {
	calls   atomic.Int32
	failure error
}

func (h *lostLockoutFinish) DialHook(next driver.DialHook) driver.DialHook { return next }
func (h *lostLockoutFinish) ProcessPipelineHook(next driver.ProcessPipelineHook) driver.ProcessPipelineHook {
	return next
}
func (h *lostLockoutFinish) ProcessHook(next driver.ProcessHook) driver.ProcessHook {
	return func(ctx context.Context, cmd driver.Cmder) error {
		args := cmd.Args()
		matches := len(args) > 4 && args[0] == "eval" && args[1] == authLockoutScript && args[4] == "finish"
		err := next(ctx, cmd)
		if matches && err == nil {
			h.calls.Add(1)
			return h.failure
		}
		return err
	}
}
func TestLockoutLostFinishAcknowledgementDoesNotRetryOrGrantAuthority(t *testing.T) {
	c, key := lockoutFixture(t)
	p := lockout.DefaultPolicy()
	p.MaxFailures = 1
	k := key("lost-finish")
	snapshot := lockouttest.Begin(t, c, k, p)
	hook := &lostLockoutFinish{failure: errors.New("hidden successful response")}
	c.raw.AddHook(hook)
	d, err := c.LockoutFinish(t.Context(), k, p, snapshot, lockout.Failed)
	if err == nil || d != (lockout.Decision{}) || hook.calls.Load() != 1 {
		t.Fatal("uncertain finish retried or returned decision", d, err)
	}
	a, err := c.LockoutBegin(t.Context(), k, p, lockouttest.Generation(t))
	if err != nil || a.Decision.Status != lockout.StatusLocked {
		t.Fatal("confirmed lock was not retained", a, err)
	}
}
