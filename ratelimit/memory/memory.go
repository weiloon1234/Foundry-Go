// Package memory implements explicit single-process fixed-window rate limiting.
// It never evicts live quotas or becomes an automatic Redis failure fallback.
package memory

import (
	"container/heap"
	"context"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkadapter"
	"github.com/weiloon1234/Foundry-Go/internal/ratewindow"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
)

// bucket is one live window. index is its position in the expiry heap.
type bucket struct {
	key    ratelimit.Key
	limit  ratelimit.Limit
	offset int64
	end    int64
	used   uint32
	index  int
}

// expiries orders live buckets by window end, so a full table reclaims expired
// buckets in amortized O(log n) each instead of scanning every entry.
type expiries []*bucket

func (h expiries) Len() int           { return len(h) }
func (h expiries) Less(i, j int) bool { return h[i].end < h[j].end }
func (h expiries) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index, h[j].index = i, j
}
func (h *expiries) Push(value any) {
	item := value.(*bucket)
	item.index = len(*h)
	*h = append(*h, item)
}
func (h *expiries) Pop() any {
	old := *h
	item := old[len(old)-1]
	old[len(old)-1] = nil
	*h = old[:len(old)-1]
	item.index = -1
	return item
}

// Backend owns at most maxEntries live buckets. Key sizes and each bucket's state
// are bounded. Clock.Now must return promptly. Hot buckets are O(1); expired
// buckets are reclaimed lazily on access and, when a new bucket needs capacity,
// from an expiry-ordered heap. A backward clock reading is clamped to the latest
// reading already observed, so it can neither reopen old quota nor fail.
type Backend struct {
	mu       sync.Mutex
	clock    clock.Clock
	capacity int
	entries  map[ratelimit.Key]*bucket
	expiry   expiries
	last     int64
	closed   bool
}

// FoundryAdapter marks the backend as a framework-owned adapter.
func (*Backend) FoundryAdapter(frameworkadapter.Seal) {}

func New(maxEntries int, source clock.Clock) (*Backend, error) {
	if maxEntries <= 0 || source == nil {
		return nil, fault.New(fault.Invalid, "memory rate limiter requires positive capacity and a clock")
	}
	return &Backend{clock: source, capacity: maxEntries, entries: make(map[ratelimit.Key]*bucket)}, nil
}

var _ ratelimit.Backend = (*Backend)(nil)
var _ ratelimit.InspectBackend = (*Backend)(nil)

func (b *Backend) RateLimit(ctx context.Context, key ratelimit.Key, limit ratelimit.Limit, cost uint32) (ratelimit.Decision, error) {
	return b.decide(ctx, key, limit, cost, true)
}

// PeekRateLimit reports the decision without consuming capacity or storing state.
func (b *Backend) PeekRateLimit(ctx context.Context, key ratelimit.Key, limit ratelimit.Limit, cost uint32) (ratelimit.Decision, error) {
	return b.decide(ctx, key, limit, cost, false)
}

// ClearRateLimit removes the key's live bucket; true means one existed.
func (b *Backend) ClearRateLimit(ctx context.Context, key ratelimit.Key) (bool, error) {
	if err := ratelimit.ValidateClear(ctx, key); err != nil {
		return false, err
	}
	if b == nil || b.clock == nil {
		return false, fault.New(fault.Invalid, "memory rate limiter is not initialized")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return false, fault.New(fault.Closed, "memory rate limiter is closed")
	}
	now := b.observe()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	item := b.entries[key]
	if item == nil {
		return false, nil
	}
	b.remove(item)
	return now < item.end, nil
}

// observe returns the authority time, never earlier than a previous reading.
// Call with b.mu held.
func (b *Backend) observe() int64 {
	now := b.clock.Now().UnixMilli()
	if now < b.last {
		return b.last
	}
	b.last = now
	return now
}
func (b *Backend) remove(item *bucket) {
	delete(b.entries, item.key)
	if item.index >= 0 {
		heap.Remove(&b.expiry, item.index)
	}
}

// reclaim removes expired buckets in end order. Live buckets are never evicted.
func (b *Backend) reclaim(now int64) {
	for len(b.expiry) > 0 && now >= b.expiry[0].end {
		item := heap.Pop(&b.expiry).(*bucket)
		delete(b.entries, item.key)
	}
}

func (b *Backend) decide(ctx context.Context, key ratelimit.Key, limit ratelimit.Limit, cost uint32, consume bool) (ratelimit.Decision, error) {
	if err := ratelimit.ValidateOperation(ctx, key, limit, cost); err != nil {
		return ratelimit.Decision{}, err
	}
	if b == nil || b.clock == nil {
		return ratelimit.Decision{}, fault.New(fault.Invalid, "memory rate limiter is not initialized")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return ratelimit.Decision{}, fault.New(fault.Closed, "memory rate limiter is closed")
	}
	now := b.observe()
	window := limit.Window.Milliseconds()
	offset := key.WindowOffset(limit.Window).Milliseconds()
	fresh, err := ratewindow.End(now, window, offset)
	if err != nil {
		return ratelimit.Decision{}, err
	}
	if err := ctx.Err(); err != nil {
		return ratelimit.Decision{}, err
	}
	item := b.entries[key]
	if item != nil && now >= item.end {
		b.remove(item)
		item = nil
	}
	// Work on a copy: a peek never changes usage, expiry or policy, and a denial
	// changes them only by persisting a policy conversion that ends later.
	next := bucket{key: key, limit: limit, offset: offset, end: fresh}
	converted := false
	if item != nil {
		next.used = item.used
		if item.limit == limit {
			next.offset, next.end = item.offset, item.end
		} else {
			// A changed policy converts the live bucket; admitted usage still counts.
			// A denial persists the conversion only when it ends later, so the old
			// policy's shorter window cannot release carried usage early.
			next.used = min(next.used, limit.Requests)
			converted = true
		}
	}
	allowed := cost <= limit.Requests-next.used
	if !allowed && consume && converted && next.end > item.end {
		changed := item.end != next.end
		next.index = item.index
		*item = next
		if changed {
			heap.Fix(&b.expiry, item.index)
		}
	}
	if allowed && consume {
		next.used += cost
		if item == nil {
			if len(b.entries) >= b.capacity {
				b.reclaim(now)
			}
			if len(b.entries) >= b.capacity {
				return ratelimit.Decision{}, fault.New(fault.Conflict, "memory rate limit capacity reached")
			}
			created := next
			b.entries[key] = &created
			heap.Push(&b.expiry, &created)
		} else {
			changed := item.end != next.end
			next.index = item.index
			*item = next
			if changed {
				heap.Fix(&b.expiry, item.index)
			}
		}
	}
	decision := ratelimit.Decision{Allowed: allowed, Limit: limit.Requests, Remaining: limit.Requests - next.used, ResetAfter: time.Duration(next.end-now) * time.Millisecond}
	if !allowed {
		decision.RetryAfter = decision.ResetAfter
	}
	return decision, nil
}

// Close releases this local authority. Stop its callers first; no background work
// is started by this adapter or the borrowing Store.
func (b *Backend) Close() error {
	if b == nil || b.clock == nil {
		return fault.New(fault.Invalid, "memory rate limiter is not initialized")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	clear(b.entries)
	b.expiry = nil
	return nil
}
