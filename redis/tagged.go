package redis

import (
	"context"
	_ "embed"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/internal/cacheint"
)

//go:embed tagged_cache.lua
var taggedCacheBody string
var taggedCacheScript = cacheIntegerScript + cacheEntryScript + tagMetadataScript + taggedCacheBody

// GetTagged returns owned bytes under a current tag snapshot. A changed/missing
// snapshot returns Conflict; an older stored payload is removed conditionally.
func (c *Client) GetTagged(ctx context.Context, key cache.TaggedKey) ([]byte, bool, error) {
	return c.taggedCommand(ctx, key, "get", nil, cache.Forever())
}

// PutTagged validates the snapshot and writes an owned value with explicit expiry.
// Corruption, stale snapshots and denied mutation permissions preserve live data.
func (c *Client) PutTagged(ctx context.Context, key cache.TaggedKey, data []byte, ttl cache.TTL) error {
	_, _, err := c.taggedCommand(ctx, key, "put", data, ttl)
	return err
}

// AddTagged atomically fills an absent or invalidated entry under this snapshot.
// An existing current value retains its bytes and expiry.
func (c *Client) AddTagged(ctx context.Context, key cache.TaggedKey, data []byte, ttl cache.TTL) (bool, error) {
	_, ok, err := c.taggedCommand(ctx, key, "add", data, ttl)
	return ok, err
}

// ForgetTagged conditionally removes this tagged entry. A stale caller cannot
// remove a replacement written under a newer snapshot.
func (c *Client) ForgetTagged(ctx context.Context, key cache.TaggedKey) (bool, error) {
	_, ok, err := c.taggedCommand(ctx, key, "forget", nil, cache.Forever())
	return ok, err
}

// IncrementTagged combines snapshot validation with exact signed arithmetic.
// Existing expiry is retained; an absent/invalidated counter starts at zero.
func (c *Client) IncrementTagged(ctx context.Context, key cache.TaggedKey, delta int64, ttl cache.TTL) (int64, error) {
	data, _, err := c.taggedCommand(ctx, key, "increment", cacheint.Encode(delta), ttl)
	if err != nil {
		return 0, err
	}
	return cacheint.Decode(data)
}
func (c *Client) taggedCommand(ctx context.Context, key cache.TaggedKey, op string, data []byte, ttl cache.TTL) ([]byte, bool, error) {
	if err := key.Validate(); err != nil {
		return nil, false, err
	}
	expiry, bound, err := c.cacheArguments(ctx, op, data, ttl)
	if err != nil {
		return nil, false, err
	}
	addresses, args := taggedArguments(key, op, data, expiry, bound)
	value, err := c.execute(ctx, func(ctx context.Context, raw *driver.Client) (any, error) {
		return evalScript(ctx, raw, taggedCacheScript, addresses, args...).Result()
	})
	if err != nil {
		return nil, false, err
	}
	return decodeReply(value, op == "get" || op == "increment")
}

func taggedArguments(key cache.TaggedKey, op string, data []byte, expiry string, bound int) ([]string, []any) {
	stamps := key.Stamps()
	addresses := make([]string, 1, len(stamps)+1)
	addresses[0] = key.DataKey().String()
	args := []any{tagMetadataPrefix, cache.TagVersionBytes, op, bound, string(data), expiry, string(key.VersionFingerprint())}
	for _, stamp := range stamps {
		addresses = append(addresses, stamp.Key.String())
		args = append(args, string(stamp.Version.Bytes()))
	}
	return addresses, args
}

var _ cache.SnapshotWriteBackend = (*Client)(nil)

// PutSnapshot resolves tag metadata (creating fresh versions for missing
// metadata) and writes under the resolved snapshot in one atomic script.
func (c *Client) PutSnapshot(ctx context.Context, base cache.EntryKey, tags []cache.EntryKey, data []byte, ttl cache.TTL) error {
	_, _, err := c.snapshotCommand(ctx, base, tags, "put", data, ttl)
	return err
}

// AddSnapshot fills an absent or invalidated entry under the current snapshot.
func (c *Client) AddSnapshot(ctx context.Context, base cache.EntryKey, tags []cache.EntryKey, data []byte, ttl cache.TTL) (bool, error) {
	_, ok, err := c.snapshotCommand(ctx, base, tags, "add", data, ttl)
	return ok, err
}

// ForgetSnapshot removes the entry of the current snapshot; obsolete payloads
// are reclaimed but not counted.
func (c *Client) ForgetSnapshot(ctx context.Context, base cache.EntryKey, tags []cache.EntryKey) (bool, error) {
	_, ok, err := c.snapshotCommand(ctx, base, tags, "forget", nil, cache.Forever())
	return ok, err
}

// IncrementSnapshot applies exact signed arithmetic under the current snapshot.
func (c *Client) IncrementSnapshot(ctx context.Context, base cache.EntryKey, tags []cache.EntryKey, delta int64, ttl cache.TTL) (int64, error) {
	data, _, err := c.snapshotCommand(ctx, base, tags, "increment", cacheint.Encode(delta), ttl)
	if err != nil {
		return 0, err
	}
	return cacheint.Decode(data)
}

// ExpireSnapshot changes the expiry of the current snapshot's entry.
func (c *Client) ExpireSnapshot(ctx context.Context, base cache.EntryKey, tags []cache.EntryKey, ttl cache.TTL) (bool, error) {
	_, ok, err := c.snapshotCommand(ctx, base, tags, "expire", nil, ttl)
	return ok, err
}

// snapshotCommand runs the tagged script in resolve mode: an empty fingerprint
// followed by one fresh version per metadata key for missing metadata.
func (c *Client) snapshotCommand(ctx context.Context, base cache.EntryKey, tags []cache.EntryKey, op string, data []byte, ttl cache.TTL) ([]byte, bool, error) {
	address, err := cache.TaggedDataKey(base, tags)
	if err != nil {
		return nil, false, err
	}
	expiry, bound, err := c.cacheArguments(ctx, op, data, ttl)
	if err != nil {
		return nil, false, err
	}
	addresses := make([]string, 1, len(tags)+1)
	addresses[0] = address.String()
	args := make([]any, 7, len(tags)+7)
	args[0], args[1], args[2], args[3], args[4], args[5], args[6] = tagMetadataPrefix, cache.TagVersionBytes, op, bound, string(data), expiry, ""
	for _, key := range tags {
		version, err := cache.NewTagVersion()
		if err != nil {
			return nil, false, err
		}
		addresses = append(addresses, key.String())
		args = append(args, string(version.Bytes()))
	}
	value, err := c.execute(ctx, func(ctx context.Context, raw *driver.Client) (any, error) {
		return evalScript(ctx, raw, taggedCacheScript, addresses, args...).Result()
	})
	if err != nil {
		return nil, false, err
	}
	return decodeReply(value, op == "increment")
}
