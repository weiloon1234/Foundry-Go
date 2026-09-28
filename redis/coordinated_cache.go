package redis

import (
	"context"
	_ "embed"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/lease"
)

//go:embed cache_leased.lua
var cacheLeasedPrefix string

//go:embed tagged_leased.lua
var taggedLeasedPrefix string
var cacheLeasedScript = leaseOwnerScript + cacheLeasedPrefix + cacheScript
var taggedLeasedScript = leaseOwnerScript + taggedLeasedPrefix + taggedCacheScript
var _ cache.CoordinatedBackend = (*Client)(nil)
var _ cache.CoordinatedTaggedBackend = (*Client)(nil)

// PutLeased atomically checks this fill's current lease and publishes the value.
// Old owners cannot write after a successor has acquired the same fill address.
func (c *Client) PutLeased(ctx context.Context, key cache.EntryKey, data []byte, ttl cache.TTL, proof lease.Proof) error {
	if err := cache.ValidateFillProof(ctx, key, proof); err != nil {
		return err
	}
	expiry, bound, err := c.cacheArguments(ctx, "put", data, ttl)
	if err != nil {
		return err
	}
	value, err := c.execute(ctx, func(ctx context.Context, raw *driver.Client) (any, error) {
		return raw.Eval(ctx, cacheLeasedScript, []string{key.String(), proof.Key().String()}, "put", bound, string(data), expiry, string(proof.Owner().Bytes()), lease.OwnerBytes).Result()
	})
	if err != nil {
		return err
	}
	_, _, err = decodeReply(value, false)
	return err
}

// PutTaggedLeased checks the lease and tag snapshot in the same atomic operation.
// It reuses tagged payload/expiry validation and permission preflight.
func (c *Client) PutTaggedLeased(ctx context.Context, key cache.TaggedKey, data []byte, ttl cache.TTL, proof lease.Proof) error {
	if err := key.Validate(); err != nil {
		return err
	}
	if err := cache.ValidateFillProof(ctx, key.FillKey(), proof); err != nil {
		return err
	}
	expiry, bound, err := c.cacheArguments(ctx, "put", data, ttl)
	if err != nil {
		return err
	}
	addresses, args := taggedArguments(key, "put", data, expiry, bound)
	addresses = append(addresses, proof.Key().String())
	args = append(args, string(proof.Owner().Bytes()), lease.OwnerBytes)
	value, err := c.execute(ctx, func(ctx context.Context, raw *driver.Client) (any, error) {
		return raw.Eval(ctx, taggedLeasedScript, addresses, args...).Result()
	})
	if err != nil {
		return err
	}
	_, _, err = decodeReply(value, false)
	return err
}
