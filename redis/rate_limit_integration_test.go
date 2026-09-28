package redis

import (
	"context"
	"errors"
	"fmt"
	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ratelimittest"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func rateFixture(t *testing.T) (*Client, func(string) ratelimit.Key) {
	t.Helper()
	c, namespace, track := integrationAddresses(t, nil)
	return c, func(logical string) ratelimit.Key {
		k, err := ratelimit.NewKey(namespace, "requests", logical)
		if err != nil {
			t.Fatal(err)
		}
		track(k.String())
		return k
	}
}
func TestRateLimitSharedContract(t *testing.T) {
	ratelimittest.Run(t, func(t *testing.T) (ratelimit.Backend, func(string) ratelimit.Key) { return rateFixture(t) })
}
func TestRateLimitTwoClientsAndServerClock(t *testing.T) {
	c, key := rateFixture(t)
	other, err := Open(t.Context(), integrationConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close(context.Background()) })
	k := key("two-clients")
	limit := ratelimit.PerHour(15)
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := range 64 {
		wg.Go(func() {
			client := c
			if i%2 == 1 {
				client = other
			}
			d, err := client.RateLimit(t.Context(), k, limit, 1)
			if err != nil {
				t.Error(err)
			}
			if d.Allowed {
				allowed.Add(1)
			}
		})
	}
	wg.Wait()
	if allowed.Load() != 15 {
		t.Fatal(allowed.Load())
	}
	now := c.raw.Time(t.Context()).Val().UnixMilli()
	expiry := c.raw.PExpireTime(t.Context(), k.String()).Val().Milliseconds()
	want := now - now%limit.Window.Milliseconds() + limit.Window.Milliseconds()
	if expiry != want {
		t.Fatal("expiry not aligned to server clock", expiry, want)
	}
	isolated, err := ratelimit.NewKey(keyspace.Namespace{Application: "other", Environment: k.Namespace().Environment}, "requests", "two-clients")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.raw.Del(context.Background(), isolated.String()) })
	if d, err := c.RateLimit(t.Context(), isolated, limit, 15); err != nil || !d.Allowed {
		t.Fatal(d, err)
	}
}
func TestRateLimitCorruptMetadataIsPreserved(t *testing.T) {
	c, key := rateFixture(t)
	for _, wire := range []string{"private", strings.Repeat("x", rateLimitMetadataBytes+1), "1:0:1000:2000:1", "1:1:1000:2000:2", "1:01:1000:2000:1", "1:1:1000:2001:1", "1:1:1000:9007199254740992:1", "1:1:1000:2000:0"} {
		k := key(fmt.Sprintf("corrupt-%d", len(wire)) + wire[:1])
		if err := c.raw.Set(t.Context(), k.String(), wire, time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
		if d, err := c.RateLimit(t.Context(), k, ratelimit.PerSecond(1), 1); !errors.Is(err, fault.Invalid) || d.Allowed || strings.Contains(err.Error(), "private") {
			t.Fatal(d, err)
		}
		if c.raw.Get(t.Context(), k.String()).Val() != wire {
			t.Fatal("corruption overwritten")
		}
	}
	k := key("list")
	c.raw.LPush(t.Context(), k.String(), "private")
	if _, err := c.RateLimit(t.Context(), k, ratelimit.PerSecond(1), 1); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if c.raw.LLen(t.Context(), k.String()).Val() != 1 {
		t.Fatal("wrong type changed")
	}
	k = key("expiry")
	if _, err := c.RateLimit(t.Context(), k, ratelimit.PerHour(1), 1); err != nil {
		t.Fatal(err)
	}
	c.raw.Persist(t.Context(), k.String())
	if _, err := c.RateLimit(t.Context(), k, ratelimit.PerHour(1), 1); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}

// Clock injection changes only this test client's submitted Lua TIME expression.
// Redis executes the actual atomic storage script; no shared server clock/config changes.
type rateClockHook struct {
	millis atomic.Int64
	script string
}

func (h *rateClockHook) DialHook(next driver.DialHook) driver.DialHook { return next }
func (h *rateClockHook) ProcessPipelineHook(next driver.ProcessPipelineHook) driver.ProcessPipelineHook {
	return next
}
func (h *rateClockHook) ProcessHook(next driver.ProcessHook) driver.ProcessHook {
	return func(ctx context.Context, cmd driver.Cmder) error {
		args := cmd.Args()
		script := h.script
		if script == "" {
			script = rateLimitScript
		}
		if len(args) > 1 && args[0] == "eval" && args[1] == script {
			ms := h.millis.Load()
			args[1] = strings.Replace(script, "redis.call('TIME')", fmt.Sprintf("{'%d','%d'}", ms/1000, ms%1000*1000), 1)
		}
		return next(ctx, cmd)
	}
}
func TestRateLimitBoundaryDenialAndPolicyReplacement(t *testing.T) {
	c, key := rateFixture(t)
	now := c.raw.Time(t.Context()).Val().UnixMilli()
	// Use a future hour so Redis expiry does not race the deliberately controlled clock.
	start := now - now%time.Hour.Milliseconds() + time.Hour.Milliseconds()
	clock := &rateClockHook{}
	clock.millis.Store(start + 250)
	c.raw.AddHook(clock)
	k := key("boundary")
	limit := ratelimit.PerSecond(2)
	d, err := c.RateLimit(t.Context(), k, limit, 2)
	if err != nil || !d.Allowed || d.ResetAfter != 750*time.Millisecond {
		t.Fatal(d, err)
	}
	before := c.raw.Get(t.Context(), k.String()).Val()
	expiry := c.raw.PExpireTime(t.Context(), k.String()).Val()
	clock.millis.Store(start + 999)
	d, err = c.RateLimit(t.Context(), k, limit, 1)
	if err != nil || d.Allowed || d.RetryAfter != time.Millisecond {
		t.Fatal(d, err)
	}
	if c.raw.Get(t.Context(), k.String()).Val() != before || c.raw.PExpireTime(t.Context(), k.String()).Val() != expiry {
		t.Fatal("denial mutated bucket")
	}
	clock.millis.Store(start - 1)
	if _, err := c.RateLimit(t.Context(), k, limit, 1); !errors.Is(err, fault.Conflict) {
		t.Fatal(err)
	}
	clock.millis.Store(start + 1000)
	d, err = c.RateLimit(t.Context(), k, ratelimit.PerSecond(3), 3)
	if err != nil || !d.Allowed || d.ResetAfter != time.Second {
		t.Fatal(d, err)
	}
}

// Hide only a successfully applied admission's reply, with no command retry.
type lostRateAcknowledgement struct {
	calls   atomic.Int32
	failure error
}

func (h *lostRateAcknowledgement) DialHook(next driver.DialHook) driver.DialHook { return next }
func (h *lostRateAcknowledgement) ProcessPipelineHook(next driver.ProcessPipelineHook) driver.ProcessPipelineHook {
	return next
}
func (h *lostRateAcknowledgement) ProcessHook(next driver.ProcessHook) driver.ProcessHook {
	return func(ctx context.Context, cmd driver.Cmder) error {
		args := cmd.Args()
		matches := len(args) > 1 && args[0] == "eval" && args[1] == rateLimitScript
		err := next(ctx, cmd)
		if matches {
			h.calls.Add(1)
			if err == nil {
				return h.failure
			}
		}
		return err
	}
}
func TestRateLimitAppliedButUnacknowledgedConsumption(t *testing.T) {
	c, key := rateFixture(t)
	other, err := Open(t.Context(), integrationConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close(context.Background()) })
	hook := &lostRateAcknowledgement{failure: errors.New("lost acknowledgement")}
	c.raw.AddHook(hook)
	k := key("unknown-consumption")
	limit := ratelimit.PerHour(2)
	d, err := c.RateLimit(t.Context(), k, limit, 1)
	if !errors.Is(err, hook.failure) || d != (ratelimit.Decision{}) || hook.calls.Load() != 1 {
		t.Fatal(d, err, hook.calls.Load())
	}
	d, err = other.RateLimit(t.Context(), k, limit, 1)
	if err != nil || !d.Allowed || d.Remaining != 0 {
		t.Fatal("lost reply changed consumption", d, err)
	}
}
