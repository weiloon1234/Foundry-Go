package redis

import (
	"context"
	"errors"
	"fmt"
	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ratelimittest"
	"github.com/weiloon1234/Foundry-Go/internal/ratewindow"
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
	want, err := ratewindow.End(now, limit.Window.Milliseconds(), k.WindowOffset(limit.Window).Milliseconds())
	if err != nil || expiry != want {
		t.Fatal("expiry not aligned to the key's phase on the server clock", expiry, want, err)
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
	for _, wire := range []string{"private", strings.Repeat("x", rateLimitMetadataBytes+1), "1:0:1000:2000:1", "1:1:1000:2000:2", "1:01:1000:2000:1", "1:1:1000:2001:1", "1:1:1000:9007199254740992:1", "1:1:1000:2000:0",
		"2:1:1000:1000:3000:1", "2:1:1000:5:3000:1", "2:1:1000:05:3005:1", "2:1:1000:5:3005", "3:1:1000:0:3000:1"} {
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
		if runsScript(args, script) {
			ms := h.millis.Load()
			forceEval(args, strings.Replace(script, "redis.call('TIME')", fmt.Sprintf("{'%d','%d'}", ms/1000, ms%1000*1000), 1))
		}
		return next(ctx, cmd)
	}
}
func TestRateLimitBoundaryDenialAndPolicyReplacement(t *testing.T) {
	c, key := rateFixture(t)
	k := key("boundary")
	limit := ratelimit.PerSecond(2)
	now := c.raw.Time(t.Context()).Val().UnixMilli()
	// Use the key's phased window in a future hour so Redis expiry does not race
	// the deliberately controlled script clock.
	hour := now - now%time.Hour.Milliseconds() + time.Hour.Milliseconds()
	start := hour + k.WindowOffset(time.Second).Milliseconds()
	clock := &rateClockHook{}
	clock.millis.Store(start + 250)
	c.raw.AddHook(clock)
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
	unchanged := func(message string) {
		t.Helper()
		if c.raw.Get(t.Context(), k.String()).Val() != before || c.raw.PExpireTime(t.Context(), k.String()).Val() != expiry {
			t.Fatal(message)
		}
	}
	unchanged("denial mutated bucket")
	// A backward step is clamped to the bucket start: no failure, no reopened quota.
	clock.millis.Store(start - 1)
	d, err = c.RateLimit(t.Context(), k, limit, 1)
	if err != nil || d.Allowed || d.RetryAfter != time.Second {
		t.Fatal(d, err)
	}
	unchanged("backward clock mutated bucket")
	// Peek never mutates, even when the policy differs.
	clock.millis.Store(start + 500)
	d, err = c.PeekRateLimit(t.Context(), k, ratelimit.PerSecond(3), 1)
	if err != nil || !d.Allowed || d.Remaining != 1 || d.ResetAfter != 500*time.Millisecond {
		t.Fatal(d, err)
	}
	unchanged("peek mutated bucket")
	// A live policy change converts the bucket, keeping admitted usage.
	d, err = c.RateLimit(t.Context(), k, ratelimit.PerSecond(3), 1)
	if err != nil || !d.Allowed || d.Remaining != 0 || d.ResetAfter != 500*time.Millisecond {
		t.Fatal(d, err)
	}
	if got := c.raw.Get(t.Context(), k.String()).Val(); got != fmt.Sprintf("2:3:1000:%d:%d:3", k.WindowOffset(time.Second).Milliseconds(), start+1000) {
		t.Fatal("converted wire", got)
	}
	clock.millis.Store(start + 1000)
	d, err = c.RateLimit(t.Context(), k, ratelimit.PerSecond(3), 3)
	if err != nil || !d.Allowed || d.ResetAfter != time.Second {
		t.Fatal(d, err)
	}
}
func TestRateLimitVersionOneBucketsRemainCompatible(t *testing.T) {
	c, key := rateFixture(t)
	k := key("version-one")
	window := ratelimit.MaxWindow
	now := c.raw.Time(t.Context()).Val().UnixMilli()
	end := now - now%window.Milliseconds() + window.Milliseconds()
	if end-now < 10000 {
		t.Skip("too close to the epoch window boundary for a stable fixture")
	}
	// An epoch-aligned version 1 bucket written by an earlier release.
	wire := fmt.Sprintf("1:2:%d:%d:1", window.Milliseconds(), end)
	if err := c.raw.Set(t.Context(), k.String(), wire, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := c.raw.PExpireAt(t.Context(), k.String(), time.UnixMilli(end)).Err(); err != nil {
		t.Fatal(err)
	}
	limit := ratelimit.Limit{Requests: 2, Window: window}
	d, err := c.RateLimit(t.Context(), k, limit, 1)
	if err != nil || !d.Allowed || d.Remaining != 0 || d.ResetAfter > time.Duration(end-now)*time.Millisecond {
		t.Fatal(d, err)
	}
	if got := c.raw.Get(t.Context(), k.String()).Val(); got != fmt.Sprintf("2:2:%d:0:%d:2", window.Milliseconds(), end) {
		t.Fatal("version 1 usage or expiry was not preserved", got)
	}
	if d, err := c.RateLimit(t.Context(), k, limit, 1); err != nil || d.Allowed {
		t.Fatal("migrated bucket reopened quota", d, err)
	}
}
func TestRateLimitClearRemovesUnreadableState(t *testing.T) {
	c, key := rateFixture(t)
	k := key("clear-corrupt")
	if err := c.raw.Set(t.Context(), k.String(), "private", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PeekRateLimit(t.Context(), k, ratelimit.PerSecond(1), 1); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if cleared, err := c.ClearRateLimit(t.Context(), k); err != nil || !cleared {
		t.Fatal(cleared, err)
	}
	if d, err := c.RateLimit(t.Context(), k, ratelimit.PerSecond(1), 1); err != nil || !d.Allowed {
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
		matches := runsScript(args, rateLimitScript)
		err := next(ctx, cmd)
		// A NOSCRIPT reply proves the script did not run; its EVAL fallback is the attempt.
		if matches && (err == nil || !strings.HasPrefix(err.Error(), "NOSCRIPT")) {
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

// Tightening 10 per minute to 10 per hour after the quota is spent keeps the
// key limited for the hour: the denied request persists the longer conversion.
func TestRateLimitDeniedPolicyConversionKeepsLaterExpiry(t *testing.T) {
	c, key := rateFixture(t)
	k := key("tightened")
	now := c.raw.Time(t.Context()).Val().UnixMilli()
	// A future hour keeps real Redis expiry from racing the controlled clock. The
	// minute window starts at the beginning of the key's hour window, so the
	// hour ends later than the minute.
	hour := now - now%time.Hour.Milliseconds() + time.Hour.Milliseconds()
	hourStart := hour + k.WindowOffset(time.Hour).Milliseconds()
	minute, offset := time.Minute.Milliseconds(), k.WindowOffset(time.Minute).Milliseconds()
	start := hourStart + ((offset-hourStart)%minute+minute)%minute
	clock := &rateClockHook{}
	clock.millis.Store(start + 1000)
	c.raw.AddHook(clock)
	if d, err := c.RateLimit(t.Context(), k, ratelimit.PerMinute(10), 10); err != nil || !d.Allowed {
		t.Fatal(d, err)
	}
	d, err := c.RateLimit(t.Context(), k, ratelimit.PerHour(10), 1)
	if err != nil || d.Allowed || d.RetryAfter <= time.Minute {
		t.Fatal("converted denial did not use the hour window", d, err)
	}
	if got := c.raw.Get(t.Context(), k.String()).Val(); !strings.HasPrefix(got, "2:10:3600000:") {
		t.Fatal("denied conversion was not persisted", got)
	}
	clock.millis.Store(start + 2*time.Minute.Milliseconds())
	if d, err := c.RateLimit(t.Context(), k, ratelimit.PerHour(10), 1); err != nil || d.Allowed {
		t.Fatal("quota reopened when the old minute ended", d, err)
	}
}
