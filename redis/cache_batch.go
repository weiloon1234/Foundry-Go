package redis

import (
	"context"
	_ "embed"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
)

//go:embed cache_batch.lua
var cacheBatchBody string
var cacheBatchScript = cacheEntryScript + cacheBatchBody
var taggedBatchScript = cacheEntryScript + tagMetadataScript + cacheBatchBody

//go:embed cache_read_batch.lua
var cacheReadBatchBody string
var cacheReadBatchScript = cacheEntryScript + cacheReadBatchBody
var taggedReadBatchScript = cacheEntryScript + tagMetadataScript + cacheReadBatchBody

var _ cache.BatchReadBackend = (*Client)(nil)
var _ cache.TaggedBatchReadBackend = (*Client)(nil)

var _ cache.BatchBackend = (*Client)(nil)
var _ cache.TaggedBatchBackend = (*Client)(nil)

// ForgetMany atomically deletes a bounded canonical batch after validating every
// stored entry. A lost reply is an uncertain outcome, never a reason to retry.
func (c *Client) ForgetMany(ctx context.Context, keys []cache.EntryKey) (uint64, error) {
	if err := cache.ValidateBatchKeys(keys); err != nil {
		return 0, err
	}
	return c.removeCacheBatch(ctx, keys, nil)
}

// ForgetManyTagged checks one shared tag snapshot and every selected payload
// before one exact DEL. Stale physical entries are excluded from the live count.
func (c *Client) ForgetManyTagged(ctx context.Context, keys []cache.TaggedKey) (uint64, error) {
	if err := cache.ValidateTaggedBatch(keys); err != nil {
		return 0, err
	}
	if len(keys) == 0 {
		return c.removeCacheBatch(ctx, nil, nil)
	}
	bases := make([]cache.EntryKey, len(keys))
	for i, key := range keys {
		bases[i] = key.DataKey()
	}
	return c.removeCacheBatch(ctx, bases, &keys[0])
}
func (c *Client) removeCacheBatch(ctx context.Context, keys []cache.EntryKey, snapshot *cache.TaggedKey) (uint64, error) {
	_, bound, err := c.cacheArguments(ctx, "forget", nil, cache.Forever())
	if err != nil {
		return 0, err
	}
	value, err := c.execute(ctx, func(ctx context.Context, raw *driver.Client) (any, error) {
		if len(keys) == 0 {
			return []any{int64(1), int64(0)}, nil
		}
		addresses := make([]string, len(keys))
		for i, key := range keys {
			addresses[i] = key.String()
		}
		script := cacheBatchScript
		args := []any{tagMetadataPrefix, cache.TagVersionBytes, bound, len(keys), ""}
		if snapshot != nil {
			script = taggedBatchScript
			args[4] = string(snapshot.VersionFingerprint())
			for _, stamp := range snapshot.Stamps() {
				addresses = append(addresses, stamp.Key.String())
				args = append(args, string(stamp.Version.Bytes()))
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return evalScript(ctx, raw, script, addresses, args...).Result()
	})
	if err != nil {
		return 0, err
	}
	fields, code, err := cacheReply(value)
	if err != nil {
		return 0, err
	}
	if code != 1 || len(fields) != 1 {
		return 0, fault.New(fault.Internal, "invalid Redis cache batch reply")
	}
	count, ok := fields[0].(int64)
	if !ok || count < 0 || count > int64(len(keys)) {
		return 0, fault.New(fault.Internal, "invalid Redis cache batch count")
	}
	return uint64(count), nil
}

// GetMany reads a bounded canonical batch in one script; results follow input
// order. Missing, expired, over-bound and wrong-type entries are misses.
func (c *Client) GetMany(ctx context.Context, keys []cache.EntryKey) ([]cache.BatchValue, error) {
	if err := cache.ValidateBatchKeys(keys); err != nil {
		return nil, err
	}
	return c.readCacheBatch(ctx, keys, nil)
}

// GetManyTagged checks one shared snapshot and reads every payload in the same
// script. Obsolete payloads are misses and are reclaimed.
func (c *Client) GetManyTagged(ctx context.Context, keys []cache.TaggedKey) ([]cache.BatchValue, error) {
	if err := cache.ValidateTaggedBatch(keys); err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return c.readCacheBatch(ctx, nil, nil)
	}
	bases := make([]cache.EntryKey, len(keys))
	for i, key := range keys {
		bases[i] = key.DataKey()
	}
	return c.readCacheBatch(ctx, bases, &keys[0])
}
func (c *Client) readCacheBatch(ctx context.Context, keys []cache.EntryKey, snapshot *cache.TaggedKey) ([]cache.BatchValue, error) {
	_, bound, err := c.cacheArguments(ctx, "get", nil, cache.Forever())
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return []cache.BatchValue{}, nil
	}
	value, err := c.execute(ctx, func(ctx context.Context, raw *driver.Client) (any, error) {
		addresses := make([]string, len(keys))
		for i, key := range keys {
			addresses[i] = key.String()
		}
		script := cacheReadBatchScript
		args := []any{tagMetadataPrefix, cache.TagVersionBytes, bound, len(keys), ""}
		if snapshot != nil {
			script = taggedReadBatchScript
			args[4] = string(snapshot.VersionFingerprint())
			for _, stamp := range snapshot.Stamps() {
				addresses = append(addresses, stamp.Key.String())
				args = append(args, string(stamp.Version.Bytes()))
			}
		}
		return evalScript(ctx, raw, script, addresses, args...).Result()
	})
	if err != nil {
		return nil, err
	}
	fields, code, err := cacheReply(value)
	if err != nil {
		return nil, err
	}
	if code != 1 || len(fields) != len(keys) {
		return nil, fault.New(fault.Internal, "invalid Redis cache batch reply")
	}
	values := make([]cache.BatchValue, len(keys))
	for i, field := range fields {
		switch item := field.(type) {
		case string:
			values[i] = cache.BatchValue{Data: []byte(item), Found: true}
		case int64:
			if item != 0 {
				return nil, fault.New(fault.Internal, "invalid Redis cache batch entry")
			}
		default:
			return nil, fault.New(fault.Internal, "invalid Redis cache batch entry")
		}
	}
	return values, nil
}
