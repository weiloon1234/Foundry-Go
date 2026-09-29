package redis

import (
	"context"
	_ "embed"
	"math"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ratewindow"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
)

//go:embed rate_limit.lua
var rateLimitScript string

const rateLimitMetadataBytes = 128

var _ ratelimit.Backend = (*Client)(nil)
var _ ratelimit.InspectBackend = (*Client)(nil)

// RateLimit makes one atomic decision using Redis TIME and the key's phased
// fixed windows. A live bucket with a different policy is converted, keeping its
// admitted usage; corrupt metadata fails without mutation. Lost replies never
// trigger retry; their capacity consumption remains unknown to the caller.
func (c *Client) RateLimit(ctx context.Context, key ratelimit.Key, limit ratelimit.Limit, cost uint32) (ratelimit.Decision, error) {
	if err := ratelimit.ValidateOperation(ctx, key, limit, cost); err != nil {
		return ratelimit.Decision{}, err
	}
	decision, err := c.rateLimit(ctx, key, limit, cost, "take")
	if err != nil {
		return ratelimit.Decision{}, err
	}
	if err := decision.Validate(limit, cost); err != nil {
		return ratelimit.Decision{}, err
	}
	return decision, nil
}

// PeekRateLimit runs the same atomic script read-only: it reports the decision
// cost would receive now without consuming capacity or changing expiry.
func (c *Client) PeekRateLimit(ctx context.Context, key ratelimit.Key, limit ratelimit.Limit, cost uint32) (ratelimit.Decision, error) {
	if err := ratelimit.ValidateOperation(ctx, key, limit, cost); err != nil {
		return ratelimit.Decision{}, err
	}
	decision, err := c.rateLimit(ctx, key, limit, cost, "peek")
	if err != nil {
		return ratelimit.Decision{}, err
	}
	if err := decision.ValidatePeek(limit, cost); err != nil {
		return ratelimit.Decision{}, err
	}
	return decision, nil
}

// ClearRateLimit deletes the key's bucket, including unreadable state at its
// exact address. A lost reply leaves the deletion unconfirmed; it is not retried.
func (c *Client) ClearRateLimit(ctx context.Context, key ratelimit.Key) (bool, error) {
	if err := ratelimit.ValidateClear(ctx, key); err != nil {
		return false, err
	}
	result, err := c.execute(ctx, func(ctx context.Context, raw *driver.Client) (any, error) {
		return raw.Del(ctx, key.String()).Result()
	})
	if err != nil {
		return false, err
	}
	removed, ok := result.(int64)
	if !ok || removed < 0 || removed > 1 {
		return false, fault.New(fault.Internal, "invalid Redis rate limit clear reply")
	}
	return removed == 1, nil
}

func (c *Client) rateLimit(ctx context.Context, key ratelimit.Key, limit ratelimit.Limit, cost uint32, mode string) (ratelimit.Decision, error) {
	offset := key.WindowOffset(limit.Window).Milliseconds()
	result, err := c.execute(ctx, func(ctx context.Context, raw *driver.Client) (any, error) {
		return evalScript(ctx, raw, rateLimitScript, []string{key.String()}, limit.Requests, limit.Window.Milliseconds(), cost, rateLimitMetadataBytes, math.MaxUint32, ratelimit.MaxWindow.Milliseconds(), ratewindow.MaxTimestamp, offset, mode).Result()
	})
	if err != nil {
		return ratelimit.Decision{}, err
	}
	parts, ok := result.([]any)
	if !ok || len(parts) == 0 {
		return ratelimit.Decision{}, fault.New(fault.Internal, "invalid Redis rate limit reply")
	}
	status, ok := parts[0].(int64)
	if !ok {
		return ratelimit.Decision{}, fault.New(fault.Internal, "invalid Redis rate limit status")
	}
	if len(parts) == 1 {
		switch status {
		case -1:
			return ratelimit.Decision{}, fault.New(fault.Invalid, "stored Redis rate limit metadata is corrupt")
		case -3:
			return ratelimit.Decision{}, fault.New(fault.Invalid, "Redis rate limit clock outside supported epoch range")
		}
	}
	if len(parts) != 3 || (status != 0 && status != 1) {
		return ratelimit.Decision{}, fault.New(fault.Internal, "invalid Redis rate limit result")
	}
	remaining, okRemaining := parts[1].(int64)
	reset, okReset := parts[2].(int64)
	if !okRemaining || !okReset || remaining < 0 || remaining > math.MaxUint32 || reset <= 0 || reset > limit.Window.Milliseconds() {
		return ratelimit.Decision{}, fault.New(fault.Internal, "invalid Redis rate limit bounds")
	}
	decision := ratelimit.Decision{Allowed: status == 1, Limit: limit.Requests, Remaining: uint32(remaining), ResetAfter: time.Duration(reset) * time.Millisecond}
	if !decision.Allowed {
		decision.RetryAfter = decision.ResetAfter
	}
	return decision, nil
}
