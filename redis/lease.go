package redis

import (
	"context"
	_ "embed"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/lease"
)

//go:embed lease_owner.lua
var leaseOwnerScript string

//go:embed lease.lua
var leaseBody string
var leaseScript = leaseOwnerScript + leaseBody
var _ lease.Backend = (*Client)(nil)

// LeaseAcquire attempts once against this Redis authority. No mutation is retried.
func (c *Client) LeaseAcquire(ctx context.Context, key lease.Key, owner lease.Owner, ttl time.Duration) (bool, error) {
	return c.leaseCommand(ctx, key, owner, ttl, "acquire")
}

// LeaseRenew extends only the matching live owner; it cannot recreate an expired key.
func (c *Client) LeaseRenew(ctx context.Context, key lease.Key, owner lease.Owner, ttl time.Duration) (bool, error) {
	return c.leaseCommand(ctx, key, owner, ttl, "renew")
}

// LeaseRelease deletes only the matching live owner. Expiry/replacement returns false.
func (c *Client) LeaseRelease(ctx context.Context, key lease.Key, owner lease.Owner) (bool, error) {
	return c.leaseCommand(ctx, key, owner, lease.MinDuration, "release")
}
func (c *Client) leaseCommand(ctx context.Context, key lease.Key, owner lease.Owner, ttl time.Duration, op string) (bool, error) {
	if err := lease.ValidateOperation(ctx, key, owner, ttl); err != nil {
		return false, err
	}
	result, err := c.execute(ctx, func(ctx context.Context, raw *driver.Client) (any, error) {
		return raw.Eval(ctx, leaseScript, []string{key.String()}, op, string(owner.Bytes()), positiveMilliseconds(ttl), lease.OwnerBytes).Result()
	})
	if err != nil {
		return false, err
	}
	status, ok := result.(int64)
	if !ok {
		return false, fault.New(fault.Internal, "invalid Redis lease reply")
	}
	switch status {
	case 0:
		return false, nil
	case 1:
		return true, nil
	case -1:
		return false, fault.New(fault.Invalid, "stored Redis lease owner is corrupt")
	}
	return false, fault.New(fault.Internal, "invalid Redis lease status")
}
