// Package admission bounds concurrent work with a short, cancellable wait.
//
// Bursts wait in FIFO order for a slot instead of failing immediately. A wait
// that cannot be satisfied reports fault.Overloaded, which transports map to a
// retryable "service unavailable" response rather than an internal error.
package admission

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// DefaultWait bounds how long an operation queues for capacity when its owner
// has no narrower policy. The caller's context deadline always applies too.
const DefaultWait = 5 * time.Second

// Wait returns the admission wait for an operation bounded by timeout.
func Wait(timeout time.Duration) time.Duration {
	if timeout <= 0 || timeout > DefaultWait {
		return DefaultWait
	}
	return timeout
}

// Semaphore is a fixed-capacity slot pool. The zero value is unusable; use New.
type Semaphore struct {
	slots chan struct{}
}

// New returns a semaphore with maximum slots. maximum must be positive.
func New(maximum int) *Semaphore {
	if maximum < 1 {
		maximum = 1
	}
	return &Semaphore{slots: make(chan struct{}, maximum)}
}

// TryAcquire takes a slot only when one is immediately available.
func (s *Semaphore) TryAcquire() bool {
	select {
	case s.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

// Acquire waits for a slot until ctx ends, stop closes or wait elapses. An
// expired wait reports fault.Overloaded; a closed stop reports fault.Closed.
// The caller's context cause remains visible through errors.Is.
func (s *Semaphore) Acquire(ctx context.Context, wait time.Duration, stop <-chan struct{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-stop:
		return fault.New(fault.Closed, "operation admission is closed")
	default:
	}
	if s.TryAcquire() {
		return nil
	}
	if wait <= 0 {
		return fault.New(fault.Overloaded, "operation capacity is exhausted")
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case s.slots <- struct{}{}:
		return nil
	case <-stop:
		return fault.New(fault.Closed, "operation admission is closed")
	case <-timer.C:
		return fault.New(fault.Overloaded, "operation capacity is exhausted")
	case <-ctx.Done():
		return fault.Wrap(fault.Overloaded, "operation capacity wait ended", ctx.Err())
	}
}

// Release returns one slot. Each successful acquisition releases exactly once.
func (s *Semaphore) Release() { <-s.slots }

// Active reports the currently held slots.
func (s *Semaphore) Active() int { return len(s.slots) }

// Capacity reports the maximum slots.
func (s *Semaphore) Capacity() int { return cap(s.slots) }
