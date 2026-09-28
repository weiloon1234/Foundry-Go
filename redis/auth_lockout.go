package redis

import (
	"context"
	_ "embed"
	"math"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ratewindow"
)

//go:embed auth_lockout.lua
var authLockoutScript string

const lockoutMetadataBytes = 384

var _ lockout.Backend = (*Client)(nil)

// LockoutBegin atomically checks the shared failure window using Redis TIME.
// This client owns no process-local account state and never retries a lost reply.
func (c *Client) LockoutBegin(ctx context.Context, key lockout.Key, policy lockout.Policy, candidate lockout.Generation) (lockout.Admission, error) {
	if err := candidate.Validate(); err != nil {
		return lockout.Admission{}, err
	}
	parts, err := c.lockoutCommand(ctx, key, policy, "begin", lockout.Snapshot{Generation: candidate}, 0)
	if err != nil {
		return lockout.Admission{}, err
	}
	var result lockout.Admission
	if len(parts) == 3 && parts[0] == int64(1) {
		text, ok := parts[1].(string)
		revision, number := parts[2].(int64)
		if !ok || !number || revision < 0 || revision > math.MaxUint32 {
			return result, invalidLockoutReply()
		}
		generation, err := lockout.ParseGeneration(text)
		if err != nil {
			return result, invalidLockoutReply()
		}
		result = lockout.Admission{Decision: lockout.Decision{Status: lockout.StatusAllowed}, Snapshot: lockout.Snapshot{Generation: generation, Revision: uint32(revision)}}
	} else if len(parts) == 2 && parts[0] == int64(2) {
		retry, ok := parts[1].(int64)
		if !ok || retry < 1 || retry > policy.LockFor.Milliseconds() {
			return result, invalidLockoutReply()
		}
		result.Decision = lockout.Decision{Status: lockout.StatusLocked, RetryAfter: time.Duration(retry) * time.Millisecond}
	} else {
		return result, invalidLockoutReply()
	}
	if err := result.Validate(policy); err != nil {
		return lockout.Admission{}, err
	}
	return result, nil
}
func (c *Client) LockoutFinish(ctx context.Context, key lockout.Key, policy lockout.Policy, snapshot lockout.Snapshot, outcome lockout.Outcome) (lockout.Decision, error) {
	if err := snapshot.Validate(); err != nil {
		return lockout.Decision{}, err
	}
	if err := outcome.Validate(); err != nil {
		return lockout.Decision{}, err
	}
	parts, err := c.lockoutCommand(ctx, key, policy, "finish", snapshot, outcome)
	if err != nil {
		return lockout.Decision{}, err
	}
	var result lockout.Decision
	switch {
	case len(parts) == 1 && parts[0] == int64(1):
		result.Status = lockout.StatusAllowed
	case len(parts) == 1 && parts[0] == int64(3):
		result.Status = lockout.StatusExpired
	case len(parts) == 3 && parts[0] == int64(2):
		retry, ok := parts[1].(int64)
		triggered, flag := parts[2].(int64)
		if !ok || retry < 1 || retry > policy.LockFor.Milliseconds() || !flag || (triggered != 0 && triggered != 1) || (outcome == lockout.Succeeded && triggered == 1) {
			return result, invalidLockoutReply()
		}
		result = lockout.Decision{Status: lockout.StatusLocked, RetryAfter: time.Duration(retry) * time.Millisecond, Triggered: triggered == 1}
	default:
		return result, invalidLockoutReply()
	}
	if err := result.Validate(policy); err != nil {
		return lockout.Decision{}, err
	}
	return result, nil
}
func (c *Client) LockoutReset(ctx context.Context, key lockout.Key, policy lockout.Policy) (bool, error) {
	parts, err := c.lockoutCommand(ctx, key, policy, "reset", lockout.Snapshot{}, 0)
	if err != nil {
		return false, err
	}
	if len(parts) != 1 {
		return false, invalidLockoutReply()
	}
	switch parts[0] {
	case int64(0):
		return false, nil
	case int64(1):
		return true, nil
	}
	return false, invalidLockoutReply()
}
func (c *Client) lockoutCommand(ctx context.Context, key lockout.Key, policy lockout.Policy, op string, snapshot lockout.Snapshot, outcome lockout.Outcome) ([]any, error) {
	if err := lockout.ValidateOperation(ctx, key, policy); err != nil {
		return nil, err
	}
	result, err := c.execute(ctx, func(ctx context.Context, raw *driver.Client) (any, error) {
		return raw.Eval(ctx, authLockoutScript, []string{key.String()}, op, policy.MaxFailures, policy.Window.Milliseconds(), policy.LockFor.Milliseconds(), snapshot.Generation.Token(), snapshot.Revision, uint8(outcome), lockout.MaxFailures, lockout.MaxDuration.Milliseconds(), ratewindow.MaxTimestamp, uint64(math.MaxUint32), lockoutMetadataBytes).Result()
	})
	if err != nil {
		return nil, err
	}
	parts, ok := result.([]any)
	if !ok || len(parts) == 0 {
		return nil, invalidLockoutReply()
	}
	status, ok := parts[0].(int64)
	if !ok {
		return nil, invalidLockoutReply()
	}
	if len(parts) == 1 {
		switch status {
		case -1:
			return nil, fault.New(fault.Invalid, "stored Redis lockout metadata is corrupt")
		case -2:
			return nil, fault.New(fault.Conflict, "live Redis lockout policy differs")
		case -3:
			return nil, fault.New(fault.Conflict, "Redis lockout clock moved backward or exceeds bounds")
		case -4:
			return nil, fault.New(fault.Conflict, "lockout revision capacity reached")
		}
	}
	return parts, nil
}
func invalidLockoutReply() error { return fault.New(fault.Internal, "invalid Redis lockout reply") }
