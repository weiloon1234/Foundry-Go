package redis

import (
	"context"
	_ "embed"
	"strconv"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/cacheint"
	"github.com/weiloon1234/Foundry-Go/lease"
)

//go:embed cache_integer.lua
var cacheIntegerScript string

//go:embed cache.lua
var cacheBody string

var cacheScript = cacheIntegerScript + cacheEntryScript + cacheBody

var _ cache.Backend = (*Client)(nil)
var _ cache.CounterBackend = (*Client)(nil)

// Get returns an owned value, distinguishing a present empty value from a miss.
func (c *Client) Get(ctx context.Context, key cache.EntryKey) ([]byte, bool, error) {
	return c.cacheCommand(ctx, key, "get", nil, cache.Forever())
}
func (c *Client) Put(ctx context.Context, key cache.EntryKey, data []byte, ttl cache.TTL) error {
	_, _, err := c.cacheCommand(ctx, key, "put", data, ttl)
	return err
}
func (c *Client) Add(ctx context.Context, key cache.EntryKey, data []byte, ttl cache.TTL) (bool, error) {
	_, ok, err := c.cacheCommand(ctx, key, "add", data, ttl)
	return ok, err
}
func (c *Client) Forget(ctx context.Context, key cache.EntryKey) (bool, error) {
	_, ok, err := c.cacheCommand(ctx, key, "forget", nil, cache.Forever())
	return ok, err
}

// Increment uses server integer arithmetic and retains an existing entry's TTL.
// Missing entries begin at zero and receive initialTTL. No mutation is retried.
func (c *Client) Increment(ctx context.Context, key cache.EntryKey, delta int64, initialTTL cache.TTL) (int64, error) {
	value, _, err := c.cacheCommand(ctx, key, "increment", cacheint.Encode(delta), initialTTL)
	if err != nil {
		return 0, err
	}
	return cacheint.Decode(value)
}
func (c *Client) cacheCommand(ctx context.Context, key cache.EntryKey, op string, data []byte, ttl cache.TTL) ([]byte, bool, error) {
	if err := key.Validate(); err != nil {
		return nil, false, err
	}
	expiry, bound, err := c.cacheArguments(ctx, op, data, ttl)
	if err != nil {
		return nil, false, err
	}
	value, err := c.execute(ctx, func(ctx context.Context, raw *driver.Client) (any, error) {
		// EVAL is intentional: one command, no NOSCRIPT fallback or mutation retries.
		return raw.Eval(ctx, cacheScript, []string{key.String()}, op, bound, string(data), expiry).Result()
	})
	if err != nil {
		return nil, false, err
	}
	return decodeReply(value, op == "get" || op == "increment")
}

// cacheArguments owns validation shared by plain and tagged operations.
func (c *Client) cacheArguments(ctx context.Context, op string, data []byte, ttl cache.TTL) (string, int, error) {
	if err := c.valid(ctx); err != nil {
		return "", 0, err
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	expiry, err := milliseconds(ttl)
	if err != nil {
		return "", 0, err
	}
	if len(data) > c.config.MaxValueBytes {
		return "", 0, fault.New(fault.Invalid, "cache value exceeds Redis payload bound")
	}
	bound := c.config.MaxValueBytes
	if op == "increment" {
		if bound < cacheint.MaxBytes {
			return "", 0, fault.New(fault.Invalid, "Redis payload bound cannot hold every counter value")
		}
		bound = cacheint.MaxBytes
	}
	return expiry, bound, nil
}
func milliseconds(ttl cache.TTL) (string, error) {
	if err := ttl.Validate(); err != nil {
		return "", err
	}
	if ttl.IsForever() {
		return "0", nil
	}
	return positiveMilliseconds(ttl.Duration()), nil
}

// positiveMilliseconds rounds positive expiry up without overflowing Duration.
func positiveMilliseconds(duration time.Duration) string {
	return strconv.FormatInt(ceilMilliseconds(duration), 10)
}

func ceilMilliseconds(duration time.Duration) int64 {
	value := duration / time.Millisecond
	if duration%time.Millisecond != 0 {
		value++
	}
	return int64(value)
}

// cacheReply parses the shared bounded script status envelope.
func cacheReply(value any) ([]any, int64, error) {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return nil, 0, fault.New(fault.Internal, "invalid Redis cache reply")
	}
	code, ok := items[0].(int64)
	if !ok {
		return nil, 0, fault.New(fault.Internal, "invalid Redis cache status")
	}
	if code < 0 && len(items) != 1 {
		return nil, 0, fault.New(fault.Internal, "invalid Redis failure reply")
	}
	switch code {
	case -4:
		return nil, 0, lease.ErrLost
	case -1:
		return nil, 0, fault.New(fault.Invalid, "stored Redis cache data or metadata is corrupt or exceeds its bound")
	case -2:
		return nil, 0, fault.New(fault.Conflict, "Redis cache tag snapshot changed or expired")
	case -3:
		return nil, 0, fault.New(fault.Invalid, "Redis tags require Redis 7 or newer with script permission checks")
	case 0, 1:
		return items[1:], code, nil
	default:
		return nil, 0, fault.New(fault.Internal, "invalid Redis cache status")
	}
}
func decodeReply(value any, wantsValue bool) ([]byte, bool, error) {
	fields, code, err := cacheReply(value)
	if err != nil {
		return nil, false, err
	}
	if code == 0 && len(fields) == 0 {
		return nil, false, nil
	}
	if code == 1 {
		if !wantsValue && len(fields) == 0 {
			return nil, true, nil
		}
		if wantsValue && len(fields) == 1 {
			if data, ok := fields[0].(string); ok {
				return []byte(data), true, nil
			}
		}
	}
	return nil, false, fault.New(fault.Internal, "invalid Redis cache result")
}
