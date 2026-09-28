package lease

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/contextlink"
)

// TryAcquire attempts once. Contention returns (nil,false,nil). A returned guard
// owns its finite lifetime and must be released. It renews only when Renew is called.
func (l Leases[K]) TryAcquire(ctx context.Context, key K, ttl time.Duration) (*Guard, bool, error) {
	return l.Acquire(ctx, key, ttl, 0)
}

// Acquire waits only on confirmed contention, up to wait. A zero wait tries once.
// Wait expiration returns context.DeadlineExceeded; backend uncertainty is never retried.
// The guard's context retains the original caller lifetime, not the acquisition timeout.
func (l Leases[K]) Acquire(ctx context.Context, key K, ttl, wait time.Duration) (*Guard, bool, error) {
	if err := l.begin(ctx, ttl, wait); err != nil {
		return nil, false, err
	}
	g, ok, err := l.acquire(ctx, key, ttl, wait, false, l.manager.end)
	if !ok {
		l.manager.end()
	}
	return g, ok, err
}

// With runs an owned callback while automatically renewing. It returns whether
// the callback ran, plus callback/loss/cleanup errors. Cancellation or renewal
// uncertainty cancels the supplied context; return promptly when it is canceled.
// With always waits for the callback's actual exit before releasing its slot.
func (l Leases[K]) With(ctx context.Context, key K, ttl, wait time.Duration, fn func(context.Context) error) (bool, error) {
	if fn == nil {
		return false, fault.New(fault.Invalid, "lease callback is required")
	}
	return l.with(ctx, key, ttl, wait, func(ctx context.Context, _ Proof) error { return fn(ctx) })
}
func (l Leases[K]) with(ctx context.Context, key K, ttl, wait time.Duration, fn func(context.Context, Proof) error) (bool, error) {
	if fn == nil {
		return false, fault.New(fault.Invalid, "lease callback is required")
	}
	if err := l.begin(ctx, ttl, wait); err != nil {
		return false, err
	}
	defer l.manager.end()
	g, ok, err := l.acquire(ctx, key, ttl, wait, true, nil)
	if !ok {
		return false, err
	}
	ran := false
	err = callback.Isolated("lease callback", func() error {
		if err := g.Err(); err != nil {
			return err
		}
		proof, err := g.Proof()
		if err != nil {
			return err
		}
		ran = true
		return fn(g.Context(), proof)
	})
	cleanup := g.Release(context.Background())
	cause := g.Err()
	if cause == ErrReleased {
		cause = nil
	}
	return ran, errors.Join(err, cause, cleanup)
}
func (l Leases[K]) begin(ctx context.Context, ttl, wait time.Duration) error {
	if l.definition == nil {
		return fault.New(fault.Invalid, "lease handle is not initialized")
	}
	if err := l.manager.ValidateScope(ttl, wait); err != nil {
		return err
	}
	return l.manager.begin(ctx)
}
func (l Leases[K]) acquire(ctx context.Context, key K, ttl, wait time.Duration, heartbeat bool, finish func()) (*Guard, bool, error) {
	parent, unlink := contextlink.Link(ctx, l.manager.ctx)
	handedOff := false
	defer func() {
		if !handedOff {
			unlink()
		}
	}()
	budget := wait
	if budget == 0 {
		budget = l.manager.config.OperationTimeout
	}
	attempt, cancel := context.WithTimeout(parent, budget)
	defer cancel()
	var address Key
	err := callback.Isolated("lease key codec", func() error {
		if err := attempt.Err(); err != nil {
			return err
		}
		logical, err := l.definition.codec.Encode(key)
		if err != nil {
			return err
		}
		if len(logical) > l.manager.config.MaxKeyBytes {
			return fault.New(fault.Invalid, "lease key exceeds its limit")
		}
		address, err = NewKey(l.manager.config.Namespace, l.definition.name, logical)
		return err
	})
	if err != nil {
		return nil, false, err
	}
	for {
		if err := attempt.Err(); err != nil {
			return nil, false, err
		}
		owner, err := NewOwner()
		if err != nil {
			return nil, false, err
		}
		until := time.Now().Add(validity(ttl))
		command, stop := context.WithDeadline(attempt, until)
		ok, err := l.manager.command(command, func(ctx context.Context) (bool, error) {
			return l.manager.backend.LeaseAcquire(ctx, address, owner, ttl)
		})
		stop()
		if err == nil && ok && (parent.Err() != nil || !time.Now().Before(until)) {
			err = errors.Join(ErrLost, parent.Err())
		}
		if err != nil {
			// The authority may have applied a command whose response was lost. Make one
			// owner-conditional cleanup attempt; expiry bounds any unknown remaining key.
			return nil, false, errors.Join(err, l.manager.releaseOwner(address, owner))
		}
		if ok {
			g := newGuard(l.manager, parent, unlink, address, owner, ttl, until, heartbeat, finish)
			handedOff = true
			return g, true, nil
		}
		if wait == 0 {
			return nil, false, nil
		}
		// Jitter only confirmed contention; command failures never enter this loop.
		delay := l.manager.config.PollInterval
		delay = delay/2 + time.Duration(rand.Int64N(int64(delay-delay/2)))
		timer := time.NewTimer(delay)
		select {
		case <-attempt.Done():
			timer.Stop()
			return nil, false, attempt.Err()
		case <-timer.C:
		}
	}
}
func (m *Manager) command(ctx context.Context, fn func(context.Context) (bool, error)) (bool, error) {
	command, cancel := context.WithTimeout(ctx, m.config.OperationTimeout)
	defer cancel()
	if err := command.Err(); err != nil {
		return false, err
	}
	var ok bool
	err := callback.Isolated("lease backend", func() error { var err error; ok, err = fn(command); return err })
	if err == nil {
		err = command.Err()
	}
	if err != nil {
		return false, fault.Wrap(fault.Internal, "lease operation failed", err)
	}
	return ok, nil
}
func (m *Manager) releaseOwner(key Key, owner Owner) error {
	_, err := m.command(context.Background(), func(ctx context.Context) (bool, error) { return m.backend.LeaseRelease(ctx, key, owner) })
	m.cleanupError(err)
	return err
}
