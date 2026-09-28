// Package memory supplies an explicit single-process lockout authority. It is
// useful for tests and local applications; it is never a Redis failure fallback.
package memory

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/internal/ratewindow"
)

type entry struct {
	policy                   lockout.Policy
	generation               lockout.Generation
	start, last, lockedUntil int64
	failures, revision       uint32
}

func (e entry) expiry() int64 {
	if e.lockedUntil != 0 {
		return e.lockedUntil
	}
	return e.start + e.policy.Window.Milliseconds()
}
func (e entry) decision(now int64) lockout.Decision {
	if e.lockedUntil > now {
		return lockout.Decision{Status: lockout.StatusLocked, RetryAfter: time.Duration(e.lockedUntil-now) * time.Millisecond}
	}
	return lockout.Decision{Status: lockout.StatusAllowed}
}

type Backend struct {
	mu       sync.Mutex
	clock    clock.Clock
	capacity int
	entries  map[lockout.Key]entry
	closed   bool
}

func New(maxEntries int, source clock.Clock) (*Backend, error) {
	if maxEntries < 1 || credential.IsNil(source) {
		return nil, fault.New(fault.Invalid, "memory lockout needs bounded capacity and a clock")
	}
	return &Backend{clock: source, capacity: maxEntries, entries: make(map[lockout.Key]entry)}, nil
}

var _ lockout.Backend = (*Backend)(nil)

// run applies a complete state transition under one lock. A failed transition
// changes nothing; live entries are never evicted to admit a different account.
func (b *Backend) run(ctx context.Context, key lockout.Key, policy lockout.Policy, fn func(int64, entry, bool) (entry, bool, error)) error {
	if err := lockout.ValidateOperation(ctx, key, policy); err != nil {
		return err
	}
	if b == nil || b.clock == nil {
		return fault.New(fault.Invalid, "memory lockout is not initialized")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return fault.New(fault.Closed, "memory lockout is closed")
	}
	now := b.clock.Now().UnixMilli()
	if err := ctx.Err(); err != nil {
		return err
	}
	if now < 0 || now > ratewindow.MaxTimestamp-lockout.MaxDuration.Milliseconds() {
		return fault.New(fault.Conflict, "lockout authority clock outside supported range")
	}
	item, exists := b.entries[key]
	if exists && now < item.last {
		return fault.New(fault.Conflict, "lockout authority clock moved backward")
	}
	if exists && now >= item.expiry() {
		exists = false
	}
	if exists && item.policy != policy {
		return fault.New(fault.Conflict, "live lockout policy differs")
	}
	updated, keep, err := fn(now, item, exists)
	if err != nil {
		return err
	}
	if !keep {
		delete(b.entries, key)
		return nil
	}
	if !exists && len(b.entries) >= b.capacity {
		for key, item := range b.entries {
			if now >= item.expiry() {
				delete(b.entries, key)
			}
		}
		if len(b.entries) >= b.capacity {
			return fault.New(fault.Conflict, "memory lockout capacity reached")
		}
	}
	b.entries[key] = updated
	return nil
}
func (b *Backend) LockoutBegin(ctx context.Context, key lockout.Key, policy lockout.Policy, candidate lockout.Generation) (lockout.Admission, error) {
	if err := candidate.Validate(); err != nil {
		return lockout.Admission{}, err
	}
	var result lockout.Admission
	err := b.run(ctx, key, policy, func(now int64, item entry, exists bool) (entry, bool, error) {
		if !exists {
			item = entry{policy: policy, generation: candidate, start: now, last: now}
		}
		result.Decision = item.decision(now)
		if result.Decision.Status == lockout.StatusAllowed {
			result.Snapshot = lockout.Snapshot{Generation: item.generation, Revision: item.revision}
		}
		return item, true, nil
	})
	if err != nil {
		return lockout.Admission{}, err
	}
	return result, nil
}
func (b *Backend) LockoutFinish(ctx context.Context, key lockout.Key, policy lockout.Policy, snapshot lockout.Snapshot, outcome lockout.Outcome) (lockout.Decision, error) {
	if err := snapshot.Validate(); err != nil {
		return lockout.Decision{}, err
	}
	if err := outcome.Validate(); err != nil {
		return lockout.Decision{}, err
	}
	var result lockout.Decision
	err := b.run(ctx, key, policy, func(now int64, item entry, exists bool) (entry, bool, error) {
		if !exists {
			result = lockout.Decision{Status: lockout.StatusExpired}
			return entry{}, false, nil
		}
		if item.generation != snapshot.Generation {
			result = lockout.Decision{Status: lockout.StatusExpired}
			return item, true, nil
		}
		if snapshot.Revision > item.revision {
			return entry{}, false, fault.New(fault.Invalid, "lockout snapshot revision is ahead of authority")
		}
		result = item.decision(now)
		if result.Status == lockout.StatusLocked {
			return item, true, nil
		}
		if outcome == lockout.Succeeded && snapshot.Revision != item.revision {
			return item, true, nil
		}
		if item.revision == math.MaxUint32 {
			return entry{}, false, fault.New(fault.Conflict, "lockout revision capacity reached")
		}
		item.revision++
		item.last = now
		if outcome == lockout.Succeeded {
			item.failures = 0
		} else {
			item.failures++
			if item.failures == policy.MaxFailures {
				item.lockedUntil = now + policy.LockFor.Milliseconds()
			}
		}
		result = item.decision(now)
		result.Triggered = result.Status == lockout.StatusLocked
		return item, true, nil
	})
	if err != nil {
		return lockout.Decision{}, err
	}
	return result, nil
}
func (b *Backend) LockoutReset(ctx context.Context, key lockout.Key, policy lockout.Policy) (bool, error) {
	changed := false
	err := b.run(ctx, key, policy, func(_ int64, _ entry, exists bool) (entry, bool, error) { changed = exists; return entry{}, false, nil })
	if err != nil {
		return false, err
	}
	return changed, nil
}
func (b *Backend) Close() error {
	if b == nil || b.clock == nil {
		return fault.New(fault.Invalid, "memory lockout is not initialized")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	clear(b.entries)
	return nil
}
