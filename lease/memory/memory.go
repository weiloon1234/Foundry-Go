// Package memory implements explicit single-process leases. It never evicts live
// owners to make space and is never an automatic fallback for Redis failures.
package memory

import (
	"context"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/lease"
)

type entry struct {
	owner lease.Owner
	until time.Time
}

// Backend owns a bounded set of leases using Go's monotonic clock. No background
// goroutine is needed; operations purge expired entries before checking capacity.
type Backend struct {
	mu      sync.Mutex
	limit   int
	closed  bool
	entries map[lease.Key]entry
}

func New(maxEntries int) (*Backend, error) {
	if maxEntries <= 0 {
		return nil, fault.New(fault.Invalid, "memory lease capacity must be positive")
	}
	return &Backend{limit: maxEntries, entries: make(map[lease.Key]entry)}, nil
}

var _ lease.Backend = (*Backend)(nil)

func (b *Backend) LeaseAcquire(ctx context.Context, key lease.Key, owner lease.Owner, ttl time.Duration) (bool, error) {
	return b.apply(ctx, key, owner, ttl, "acquire")
}
func (b *Backend) LeaseRenew(ctx context.Context, key lease.Key, owner lease.Owner, ttl time.Duration) (bool, error) {
	return b.apply(ctx, key, owner, ttl, "renew")
}
func (b *Backend) LeaseRelease(ctx context.Context, key lease.Key, owner lease.Owner) (bool, error) {
	return b.apply(ctx, key, owner, lease.MinDuration, "release")
}
func (b *Backend) apply(ctx context.Context, key lease.Key, owner lease.Owner, ttl time.Duration, op string) (bool, error) {
	if err := lease.ValidateOperation(ctx, key, owner, ttl); err != nil {
		return false, err
	}
	if b == nil || b.entries == nil {
		return false, fault.New(fault.Invalid, "memory lease backend is not initialized")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if b.closed {
		return false, fault.New(fault.Closed, "memory lease backend is closed")
	}
	now := time.Now()
	for k, v := range b.entries {
		if !now.Before(v.until) {
			delete(b.entries, k)
		}
	}
	previous, exists := b.entries[key]
	if op == "acquire" {
		if exists {
			return false, nil
		}
		if len(b.entries) >= b.limit {
			return false, fault.New(fault.Conflict, "memory lease capacity reached")
		}
	} else if !exists || previous.owner != owner {
		return false, nil
	}
	if op == "release" {
		delete(b.entries, key)
	} else {
		b.entries[key] = entry{owner: owner, until: now.Add(ttl)}
	}
	return true, nil
}

// Close invalidates this explicit local authority. Close managers first so their
// callbacks and cleanup finish while the backend is still available.
func (b *Backend) Close() error {
	if b == nil || b.entries == nil {
		return fault.New(fault.Invalid, "memory lease backend is not initialized")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	clear(b.entries)
	return nil
}
