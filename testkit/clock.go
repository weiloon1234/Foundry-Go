package testkit

import (
	"sync"
	"time"
)

// Clock is a concurrency-safe, explicitly controlled application clock. It does
// not alter Go timers or context deadlines, so freezing it cannot hang shutdown.
type Clock struct {
	mu  sync.RWMutex
	now time.Time
}

// NewClock creates a test clock initialized to the supplied instant in UTC.
func NewClock(now time.Time) *Clock { return &Clock{now: now.UTC()} }

// Now returns the current test time under a read lock.
func (c *Clock) Now() time.Time { c.mu.RLock(); defer c.mu.RUnlock(); return c.now }

// Set sets the clock to the supplied instant in UTC under its mutex.
func (c *Clock) Set(now time.Time) { c.mu.Lock(); c.now = now.UTC(); c.mu.Unlock() }

// Advance advances the clock under its mutex.
func (c *Clock) Advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}
