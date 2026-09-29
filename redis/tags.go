package redis

import (
	"context"
	_ "embed"
	"strconv"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
)

const tagMetadataPrefix = "\x00foundry:cache:tag:v1\x00"

// tagMetadataRetention is how long tag and namespace metadata survives without
// use. Every read or write refreshes it, and it never expires before a finite
// tagged entry written under it. Idle expiry creates a fresh version on next
// use, so old entries become misses; it never resurrects stale data.
const tagMetadataRetention = 30 * 24 * time.Hour

//go:embed tag_metadata.lua
var tagMetadataBody string
var tagMetadataScript = "local tag_retention = " + strconv.FormatInt(tagMetadataRetention.Milliseconds(), 10) + "\n" + tagMetadataBody

//go:embed tag_versions.lua
var tagVersionsBody string
var tagVersionsScript = tagMetadataScript + tagVersionsBody

var _ cache.TaggedBackend = (*Client)(nil)
var _ cache.TaggedCounterBackend = (*Client)(nil)

// ResolveTags atomically creates fresh versions for missing metadata and returns
// the current versions in canonical input order. Redis tags require Redis 7+.
func (c *Client) ResolveTags(ctx context.Context, keys []cache.EntryKey) ([]cache.TagVersion, error) {
	return c.tagVersions(ctx, keys, false)
}

// InvalidateTags atomically rotates the entire selected set. An uncertain remote
// error may hide an applied rotation; callers must not infer rollback or retry.
func (c *Client) InvalidateTags(ctx context.Context, keys []cache.EntryKey) error {
	_, err := c.tagVersions(ctx, keys, true)
	return err
}
func (c *Client) tagVersions(ctx context.Context, keys []cache.EntryKey, replace bool) ([]cache.TagVersion, error) {
	if err := c.valid(ctx); err != nil {
		return nil, err
	}
	if err := cache.ValidateTagKeys(keys); err != nil {
		return nil, err
	}
	value, err := c.execute(ctx, func(ctx context.Context, raw *driver.Client) (any, error) {
		addresses := make([]string, len(keys))
		mode := "0"
		if replace {
			mode = "1"
		}
		args := make([]any, 3, len(keys)+3)
		args[0] = tagMetadataPrefix
		args[1] = cache.TagVersionBytes
		args[2] = mode
		for i, key := range keys {
			version, err := cache.NewTagVersion()
			if err != nil {
				return nil, err
			}
			addresses[i] = key.String()
			args = append(args, string(version.Bytes()))
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return evalScript(ctx, raw, tagVersionsScript, addresses, args...).Result()
	})
	if err != nil {
		return nil, err
	}
	fields, code, err := cacheReply(value)
	if err != nil {
		return nil, err
	}
	if code != 1 || len(fields) != len(keys) {
		return nil, fault.New(fault.Internal, "invalid Redis tag version reply")
	}
	versions := make([]cache.TagVersion, len(keys))
	for i, field := range fields {
		data, ok := field.(string)
		if !ok {
			return nil, fault.New(fault.Internal, "invalid Redis tag version type")
		}
		versions[i], err = cache.ParseTagVersion([]byte(data))
		if err != nil {
			return nil, err
		}
	}
	return versions, nil
}
