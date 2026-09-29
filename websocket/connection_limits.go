package websocket

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
)

// tokenBucket is the connection's inbound rate limit. Each connection owns one
// bucket in memory; no shared quota entry outlives it. It refills continuously
// at Requests per Window with a burst of Requests and measures elapsed time on
// the monotonic clock, so wall-clock steps (NTP corrections) neither refill nor
// drain it. Only the serial inbound loop uses it.
type tokenBucket struct {
	capacity, tokens, perSecond float64
	last                        time.Time
}

func newTokenBucket(limit ratelimit.Limit, now time.Time) tokenBucket {
	capacity := float64(limit.Requests)
	return tokenBucket{capacity: capacity, tokens: capacity, perSecond: capacity / limit.Window.Seconds(), last: now}
}
func (b *tokenBucket) allow(now time.Time) bool {
	if elapsed := now.Sub(b.last); elapsed > 0 {
		b.tokens = min(b.capacity, b.tokens+elapsed.Seconds()*b.perSecond)
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Caller holds hub.mu. Count a connection only once for this verified subject,
// regardless of its number of rooms or channels using that guard/provider.
func (c *connectionState) subjectAllowedLocked(subject MemberID) bool {
	connections := c.hub.subjects[subject]
	if connections[c] > 0 {
		return true
	}
	return len(connections) < c.hub.config.MaxConnectionsPerSubject
}

// trackSubjectLocked counts one pending or active subscription of a subject.
func (h *Hub) trackSubjectLocked(c *connectionState, subject MemberID, delta int) {
	if subject == "" {
		return
	}
	connections := h.subjects[subject]
	if connections == nil {
		if delta < 0 {
			return
		}
		connections = make(map[*connectionState]int)
		h.subjects[subject] = connections
	}
	if connections[c] += delta; connections[c] <= 0 {
		delete(connections, c)
		if len(connections) == 0 {
			delete(h.subjects, subject)
		}
	}
}

func (c *connectionState) withFreshScope(ctx context.Context, run func(context.Context) error) error {
	if c.hub.authentication == nil {
		return run(ctx)
	}
	scope, err := c.hub.authentication.Registry().NewScope(ctx, c.credentials)
	if err != nil {
		return err
	}
	defer scope.Close()
	return run(scope.Context())
}

// messageScope returns the connection's current authentication scope with a
// read hold that keeps a concurrent refresh from closing it mid-operation.
// Guard resolutions are cached by the scope until the next refresh replaces it.
func (c *connectionState) messageScope() (*auth.Scope, func(), error) {
	if c.hub.authentication == nil {
		return nil, func() {}, nil
	}
	for {
		c.scopeMu.RLock()
		if c.scope != nil {
			return c.scope, c.scopeMu.RUnlock, nil
		}
		c.scopeMu.RUnlock()
		c.scopeMu.Lock()
		if c.scope == nil {
			scope, err := c.hub.authentication.Registry().NewScope(c.ctx, c.credentials)
			if err != nil {
				c.scopeMu.Unlock()
				return nil, nil, err
			}
			c.scope = scope
		}
		c.scopeMu.Unlock()
	}
}
