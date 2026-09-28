package redis

import (
	"context"
	_ "embed"

	"github.com/weiloon1234/Foundry-Go/cache"
)

//go:embed expiry.lua
var expiryScript string

//go:embed cache_entries.lua
var cacheEntryBody string

var cacheEntryScript = expiryScript + cacheEntryBody

var _ cache.EntryBackend = (*Client)(nil)
var _ cache.TaggedEntryBackend = (*Client)(nil)

// Exists checks the stored type and byte bound without transferring the value.
func (c *Client) Exists(ctx context.Context, key cache.EntryKey) (bool, error) {
	_, found, err := c.cacheCommand(ctx, key, "exists", nil, cache.Forever())
	return found, err
}

// Expire changes a live entry's expiry without replacing its bytes. Existing
// persistent entries return true when Forever is requested again.
func (c *Client) Expire(ctx context.Context, key cache.EntryKey, ttl cache.TTL) (bool, error) {
	_, found, err := c.cacheCommand(ctx, key, "expire", nil, ttl)
	return found, err
}
func (c *Client) ExistsTagged(ctx context.Context, key cache.TaggedKey) (bool, error) {
	_, found, err := c.taggedCommand(ctx, key, "exists", nil, cache.Forever())
	return found, err
}
func (c *Client) ExpireTagged(ctx context.Context, key cache.TaggedKey, ttl cache.TTL) (bool, error) {
	_, found, err := c.taggedCommand(ctx, key, "expire", nil, ttl)
	return found, err
}
