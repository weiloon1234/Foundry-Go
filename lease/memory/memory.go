// Package memory implements explicit single-process leases. It never evicts live
// owners to make space and is never an automatic fallback for Redis failures.
package memory

import (
	"container/heap"
	"context"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkadapter"
	"github.com/weiloon1234/Foundry-Go/lease"
)

type entry struct {
	key   lease.Key
	owner lease.Owner
	until time.Time
	// index is the entry's position in Backend.expiry.
	index int
}

// expiryHeap orders live owners by deadline, so reclaiming expired leases pops
// only expired entries instead of scanning the whole table on every operation.
type expiryHeap []*entry

func (h expiryHeap) Len() int           { return len(h) }
func (h expiryHeap) Less(i, j int) bool { return h[i].until.Before(h[j].until) }
func (h expiryHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index, h[j].index = i, j
}
func (h *expiryHeap) Push(value any) {
	item := value.(*entry)
	item.index = len(*h)
	*h = append(*h, item)
}
func (h *expiryHeap) Pop() any {
	old := *h
	item := old[len(old)-1]
	old[len(old)-1] = nil
	*h = old[:len(old)-1]
	return item
}

// Backend owns a bounded set of leases using Go's monotonic clock. No background
// goroutine is needed: an operation checks only its own key, and a new key
// reclaims expired owners in deadline order before checking capacity.
type Backend struct {
	mu      sync.Mutex
	limit   int
	closed  bool
	entries map[lease.Key]*entry
	expiry  expiryHeap
}

func New(maxEntries int) (*Backend, error) {
	if maxEntries <= 0 {
		return nil, fault.New(fault.Invalid, "memory lease capacity must be positive")
	}
	return &Backend{limit: maxEntries, entries: make(map[lease.Key]*entry)}, nil
}

var _ lease.Backend = (*Backend)(nil)
var _ lease.ForceBackend = (*Backend)(nil)
var _ lease.TransferBackend = (*Backend)(nil)

// FoundryAdapter marks the backend as framework-owned adapter I/O.
func (*Backend) FoundryAdapter(frameworkadapter.Seal) {}

func (b *Backend) LeaseAcquire(ctx context.Context, key lease.Key, owner lease.Owner, ttl time.Duration) (bool, error) {
	return b.apply(ctx, key, owner, ttl, "acquire")
}
func (b *Backend) LeaseRenew(ctx context.Context, key lease.Key, owner lease.Owner, ttl time.Duration) (bool, error) {
	return b.apply(ctx, key, owner, ttl, "renew")
}
func (b *Backend) LeaseRelease(ctx context.Context, key lease.Key, owner lease.Owner) (bool, error) {
	return b.apply(ctx, key, owner, lease.MinDuration, "release")
}

// LeaseTransfer replaces from with to and renews, only while from owns key.
func (b *Backend) LeaseTransfer(ctx context.Context, key lease.Key, from, to lease.Owner, ttl time.Duration) (bool, error) {
	if err := to.Validate(); err != nil {
		return false, err
	}
	return b.applyWith(ctx, key, from, ttl, "transfer", to)
}

// LeaseForceRelease administratively removes key regardless of its owner.
func (b *Backend) LeaseForceRelease(ctx context.Context, key lease.Key) (bool, error) {
	if ctx == nil {
		return false, fault.New(fault.Invalid, "lease requires a context")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := key.Validate(); err != nil {
		return false, err
	}
	if b == nil || b.entries == nil {
		return false, fault.New(fault.Invalid, "memory lease backend is not initialized")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return false, fault.New(fault.Closed, "memory lease backend is closed")
	}
	item := b.live(key, time.Now())
	if item == nil {
		return false, nil
	}
	b.remove(item)
	return true, nil
}

// live returns key's unexpired owner, reclaiming it lazily when expired.
func (b *Backend) live(key lease.Key, now time.Time) *entry {
	item := b.entries[key]
	if item != nil && !now.Before(item.until) {
		b.remove(item)
		return nil
	}
	return item
}
func (b *Backend) remove(item *entry) {
	delete(b.entries, item.key)
	heap.Remove(&b.expiry, item.index)
}
func (b *Backend) reclaim(now time.Time) {
	for len(b.expiry) > 0 && !now.Before(b.expiry[0].until) {
		b.remove(b.expiry[0])
	}
}
func (b *Backend) apply(ctx context.Context, key lease.Key, owner lease.Owner, ttl time.Duration, op string) (bool, error) {
	return b.applyWith(ctx, key, owner, ttl, op, lease.Owner{})
}

// applyWith runs one owner-checked operation; next is the transfer target.
func (b *Backend) applyWith(ctx context.Context, key lease.Key, owner lease.Owner, ttl time.Duration, op string, next lease.Owner) (bool, error) {
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
	previous := b.live(key, now)
	if op == "acquire" {
		if previous != nil {
			return false, nil
		}
		if len(b.entries) >= b.limit {
			b.reclaim(now)
		}
		if len(b.entries) >= b.limit {
			return false, fault.New(fault.Conflict, "memory lease capacity reached")
		}
		item := &entry{key: key, owner: owner, until: now.Add(ttl)}
		b.entries[key] = item
		heap.Push(&b.expiry, item)
		return true, nil
	}
	if previous == nil || previous.owner != owner {
		return false, nil
	}
	if op == "release" {
		b.remove(previous)
		return true, nil
	}
	if op == "transfer" {
		previous.owner = next
	}
	previous.until = now.Add(ttl)
	heap.Fix(&b.expiry, previous.index)
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
	b.expiry = nil
	return nil
}
