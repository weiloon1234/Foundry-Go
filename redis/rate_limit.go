package redis

import (
	"context"
	_ "embed"
	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ratewindow"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	"math"
	"time"
)

//go:embed rate_limit.lua
var rateLimitScript string

const rateLimitMetadataBytes = 128

var _ ratelimit.Backend = (*Client)(nil)

// RateLimit makes one atomic decision using Redis TIME and epoch-aligned windows.
// Live policy conflicts and corrupt metadata fail without mutation. Lost replies
// never trigger retry; their capacity consumption remains unknown to the caller.
func (c *Client) RateLimit(ctx context.Context, key ratelimit.Key, limit ratelimit.Limit, cost uint32) (ratelimit.Decision, error) {
	if err := ratelimit.ValidateOperation(ctx, key, limit, cost); err != nil {
		return ratelimit.Decision{}, err
	}
	result, err := c.execute(ctx, func(ctx context.Context, raw *driver.Client) (any, error) {
		return raw.Eval(ctx, rateLimitScript, []string{key.String()}, limit.Requests, limit.Window.Milliseconds(), cost, rateLimitMetadataBytes, math.MaxUint32, ratelimit.MaxWindow.Milliseconds(), ratewindow.MaxTimestamp).Result()
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
		case -2:
			return ratelimit.Decision{}, fault.New(fault.Conflict, "live Redis rate limit policy differs")
		case -3:
			return ratelimit.Decision{}, fault.New(fault.Conflict, "Redis rate limit clock outside live window")
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
	if err := decision.Validate(limit, cost); err != nil {
		return ratelimit.Decision{}, err
	}
	return decision, nil
}
