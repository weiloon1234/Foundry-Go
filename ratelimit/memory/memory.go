// Package memory implements explicit single-process fixed-window rate limiting.
// It never evicts live quotas or becomes an automatic Redis failure fallback.
package memory

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ratewindow"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	"sync"
	"time"
)

type entry struct {
	limit ratelimit.Limit
	end   int64
	used  uint32
}

// Backend owns at most maxEntries live buckets. Key sizes and each entry's state
// are bounded. Clock.Now must return promptly; operations purge expired buckets
// lazily, and reject backward clock readings instead of reopening old quotas.
type Backend struct {
	mu       sync.Mutex
	clock    clock.Clock
	capacity int
	entries  map[ratelimit.Key]entry
	last     int64
	closed   bool
}

func New(maxEntries int, source clock.Clock) (*Backend, error) {
	if maxEntries <= 0 || source == nil {
		return nil, fault.New(fault.Invalid, "memory rate limiter requires positive capacity and a clock")
	}
	return &Backend{clock: source, capacity: maxEntries, entries: make(map[ratelimit.Key]entry)}, nil
}

var _ ratelimit.Backend = (*Backend)(nil)

func (b *Backend) RateLimit(ctx context.Context, key ratelimit.Key, limit ratelimit.Limit, cost uint32) (ratelimit.Decision, error) {
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
	now := b.clock.Now().UnixMilli()
	end, err := ratewindow.End(now, limit.Window.Milliseconds())
	if err != nil {
		return ratelimit.Decision{}, err
	}
	if err := ctx.Err(); err != nil {
		return ratelimit.Decision{}, err
	}
	if now < b.last {
		return ratelimit.Decision{}, fault.New(fault.Conflict, "rate limit authority clock moved backward")
	}
	b.last = now
	item, exists := b.entries[key]
	if exists && now >= item.end {
		delete(b.entries, key)
		exists = false
	}
	if exists && item.limit != limit {
		return ratelimit.Decision{}, fault.New(fault.Conflict, "live rate limit policy differs")
	}
	if !exists {
		// Hot buckets stay O(1). Scan other expiries only when a new bucket
		// needs capacity; never evict a live quota to admit another resource.
		if len(b.entries) >= b.capacity {
			for k, v := range b.entries {
				if now >= v.end {
					delete(b.entries, k)
				}
			}
		}
		if len(b.entries) >= b.capacity {
			return ratelimit.Decision{}, fault.New(fault.Conflict, "memory rate limit capacity reached")
		}
		item = entry{limit: limit, end: end}
	}
	allowed := cost <= limit.Requests-item.used
	if allowed {
		item.used += cost
		b.entries[key] = item
	}
	decision := ratelimit.Decision{Allowed: allowed, Limit: limit.Requests, Remaining: limit.Requests - item.used, ResetAfter: time.Duration(item.end-now) * time.Millisecond}
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
	return nil
}
