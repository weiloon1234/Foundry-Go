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
var _ lease.ForceBackend = (*Client)(nil)
var _ lease.TransferBackend = (*Client)(nil)

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

// LeaseTransfer atomically replaces the live owner from with to and renews the
// lease; a different, expired or absent owner returns false without change.
func (c *Client) LeaseTransfer(ctx context.Context, key lease.Key, from, to lease.Owner, ttl time.Duration) (bool, error) {
	if err := to.Validate(); err != nil {
		return false, err
	}
	return c.leaseCommand(ctx, key, from, ttl, "transfer", string(to.Bytes()))
}

// LeaseForceRelease administratively deletes one exact lease address whatever
// its owner, type or expiry, repairing metadata that ordinary operations reject
// as corrupt (such as a lease key without TTL). It is never used implicitly.
func (c *Client) LeaseForceRelease(ctx context.Context, key lease.Key) (bool, error) {
	if ctx == nil {
		return false, fault.New(fault.Invalid, "lease requires a context")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := key.Validate(); err != nil {
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
		return false, fault.New(fault.Internal, "invalid Redis lease removal count")
	}
	return removed == 1, nil
}

// leaseCommand runs one owner-checked script operation; extra carries the
// transfer target owner.
func (c *Client) leaseCommand(ctx context.Context, key lease.Key, owner lease.Owner, ttl time.Duration, op string, extra ...any) (bool, error) {
	if err := lease.ValidateOperation(ctx, key, owner, ttl); err != nil {
		return false, err
	}
	args := append([]any{op, string(owner.Bytes()), positiveMilliseconds(ttl), lease.OwnerBytes}, extra...)
	result, err := c.execute(ctx, func(ctx context.Context, raw *driver.Client) (any, error) {
		return evalScript(ctx, raw, leaseScript, []string{key.String()}, args...).Result()
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
