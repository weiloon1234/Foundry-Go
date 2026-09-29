package redis

import (
	"context"
	_ "embed"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
)

//go:embed tagged_read.lua
var taggedReadBody string
var taggedReadScript = cacheEntryScript + tagMetadataScript + taggedReadBody

var _ cache.SnapshotReadBackend = (*Client)(nil)

// ReadSnapshot resolves tag/namespace metadata (creating fresh versions for
// missing metadata) and reads the tagged entry in one atomic script,
// so every typed read costs one round trip. Obsolete, over-bound or malformed
// payloads are misses and are reclaimed. The reply never includes a stale value.
func (c *Client) ReadSnapshot(ctx context.Context, base cache.EntryKey, tags []cache.EntryKey, payload bool) (cache.TaggedKey, []byte, bool, error) {
	if err := c.valid(ctx); err != nil {
		return cache.TaggedKey{}, nil, false, err
	}
	if err := base.Validate(); err != nil {
		return cache.TaggedKey{}, nil, false, err
	}
	address, err := cache.TaggedDataKey(base, tags)
	if err != nil {
		return cache.TaggedKey{}, nil, false, err
	}
	fresh := make([]cache.TagVersion, len(tags))
	stamps := make([]cache.TagStamp, len(tags))
	for i, key := range tags {
		version, err := cache.NewTagVersion()
		if err != nil {
			return cache.TaggedKey{}, nil, false, err
		}
		fresh[i] = version
		stamps[i] = cache.TagStamp{Key: key}
	}
	flag := "0"
	if payload {
		flag = "1"
	}
	addresses := make([]string, 1, len(tags)+1)
	addresses[0] = address.String()
	args := make([]any, 4, len(tags)+4)
	args[0], args[1], args[2], args[3] = tagMetadataPrefix, cache.TagVersionBytes, c.config.MaxValueBytes, flag
	for i, key := range tags {
		addresses = append(addresses, key.String())
		args = append(args, string(fresh[i].Bytes()))
	}
	value, err := c.execute(ctx, func(ctx context.Context, raw *driver.Client) (any, error) {
		return evalScript(ctx, raw, taggedReadScript, addresses, args...).Result()
	})
	if err != nil {
		return cache.TaggedKey{}, nil, false, err
	}
	fields, code, err := cacheReply(value)
	if err != nil {
		return cache.TaggedKey{}, nil, false, err
	}
	extra := len(fields) - len(tags)
	if code != 1 || extra < 0 || extra > 2 || (!payload && extra == 2) || (payload && extra == 1) {
		return cache.TaggedKey{}, nil, false, fault.New(fault.Internal, "invalid Redis tag snapshot reply")
	}
	for i := range tags {
		text, ok := fields[i].(string)
		if !ok {
			return cache.TaggedKey{}, nil, false, fault.New(fault.Internal, "invalid Redis tag version type")
		}
		if stamps[i].Version, err = cache.ParseTagVersion([]byte(text)); err != nil {
			return cache.TaggedKey{}, nil, false, err
		}
	}
	key, err := cache.NewTaggedKey(base, stamps)
	if err != nil {
		return cache.TaggedKey{}, nil, false, err
	}
	if extra == 0 {
		return key, nil, false, nil
	}
	if marker, ok := fields[len(tags)].(int64); !ok || marker != 1 {
		return cache.TaggedKey{}, nil, false, fault.New(fault.Internal, "invalid Redis tag snapshot reply")
	}
	if !payload {
		return key, nil, true, nil
	}
	data, ok := fields[len(tags)+1].(string)
	if !ok {
		return cache.TaggedKey{}, nil, false, fault.New(fault.Internal, "invalid Redis cache result")
	}
	return key, []byte(data), true, nil
}
