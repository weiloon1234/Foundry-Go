package jobs

import (
	"context"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/contextlink"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
)

// OverlapMode selects what happens to a job whose overlap key is held.
type OverlapMode uint8

const (
	// ReleaseOnOverlap releases the reservation for OverlapOptions.Delay
	// without consuming an attempt. It is the default.
	ReleaseOnOverlap OverlapMode = iota
	// SkipOnOverlap completes the job successfully without running it.
	SkipOnOverlap
)

// OverlapOptions configure WithoutOverlapping. TTL is the lease duration,
// renewed at a third of it while the handler runs; Delay is the release delay
// in ReleaseOnOverlap mode.
type OverlapOptions struct {
	TTL   time.Duration
	Delay time.Duration
	Mode  OverlapMode
}

func DefaultOverlapOptions() OverlapOptions {
	return OverlapOptions{TTL: 30 * time.Second, Delay: 10 * time.Second}
}
func (o OverlapOptions) Validate() error {
	if err := lease.ValidateDuration(o.TTL); err != nil {
		return err
	}
	if o.Mode != ReleaseOnOverlap && o.Mode != SkipOnOverlap || o.Mode == ReleaseOnOverlap && (o.Delay <= 0 || o.Delay > MaxDelay) {
		return fault.New(fault.Invalid, "invalid job overlap mode or release delay")
	}
	return nil
}

// Overlap is a typed WithoutOverlapping policy. The zero value is disabled.
type Overlap[P any] struct {
	acquire func(context.Context, P) (*lease.Guard, bool, error)
	options OverlapOptions
}

func (o Overlap[P]) enabled() bool { return o.acquire != nil }

// WithoutOverlapping holds a typed lease named by key(payload) from before the
// attempt starts until the handler and its middleware actually exit, so jobs
// with the same key never run concurrently across workers. Ownership is
// owner-token conditional through the lease layer; losing the lease cancels
// the handler. A job whose key is held is released (without consuming an
// attempt) or skipped according to options.Mode. Keys resolve from the decoded
// payload; the lease backend must be shared by every worker.
func WithoutOverlapping[P, K any](leases lease.Leases[K], key func(P) K, options OverlapOptions) Overlap[P] {
	if key == nil {
		return Overlap[P]{acquire: func(context.Context, P) (*lease.Guard, bool, error) {
			return nil, false, fault.New(fault.Invalid, "job overlap requires a key resolver")
		}, options: options}
	}
	return Overlap[P]{options: options, acquire: func(ctx context.Context, payload P) (*lease.Guard, bool, error) {
		return leases.TryAcquire(ctx, key(payload), options.TTL)
	}}
}

// holdLease renews guard until stop is called and links ctx to the guard's
// lifetime, so lease loss cancels the protected handler. It returns the linked
// context and a stop function that ends renewal and releases the lease once.
func holdLease(ctx context.Context, guard *lease.Guard, ttl time.Duration) (context.Context, func()) {
	linked, unlink := contextlink.Link(ctx, guard.Context())
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(ttl / 3)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-guard.Context().Done():
				return
			case <-ticker.C:
				renew, cancel := context.WithTimeout(context.Background(), ttl/3)
				_ = guard.Renew(renew)
				cancel()
			}
		}
	}()
	var once sync.Once
	return linked, func() {
		once.Do(func() {
			close(done)
			<-finished
			unlink()
			release, cancel := context.WithTimeout(context.Background(), ttl)
			_ = guard.Release(release)
			cancel()
		})
	}
}

// Throttle is a typed ThrottleExceptions policy. The zero value is disabled.
type Throttle[P any] struct {
	peek    func(context.Context, P) (ratelimit.Decision, error)
	record  func(context.Context, P)
	backoff time.Duration
}

func (t Throttle[P]) enabled() bool { return t.peek != nil }

// ThrottleExceptions delays a job while recent handler exceptions for its key
// exhausted limiter: the limiter's Limit is the exception budget (for example
// ratelimit.PerMinute(10) allows ten exceptions per minute). While exhausted,
// the job is released for max(backoff, the limiter's retry-after) without
// consuming an attempt. Each attempt that returns a non-cancellation error
// records one exception; recording is best effort and never changes the
// attempt's outcome. The limiter's backend must support Peek (memory and Redis).
func ThrottleExceptions[P, K any](limiter ratelimit.Limiter[K], key func(P) K, backoff time.Duration) Throttle[P] {
	if key == nil {
		return Throttle[P]{backoff: backoff, peek: func(context.Context, P) (ratelimit.Decision, error) {
			return ratelimit.Decision{}, fault.New(fault.Invalid, "job exception throttle requires a key resolver")
		}, record: func(context.Context, P) {}}
	}
	return Throttle[P]{backoff: backoff,
		peek: func(ctx context.Context, payload P) (ratelimit.Decision, error) {
			return limiter.Peek(ctx, key(payload), 1)
		},
		record: func(ctx context.Context, payload P) { _, _ = limiter.Take(ctx, key(payload), 1) },
	}
}
func (t Throttle[P]) validate() error {
	if t.backoff < 0 || t.backoff > MaxDelay {
		return fault.New(fault.Invalid, "invalid job exception throttle backoff")
	}
	return nil
}
