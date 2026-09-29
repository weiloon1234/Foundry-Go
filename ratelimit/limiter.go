package ratelimit

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// Limiter consumes capacity using only its declaration's key type. Its zero value
// is invalid. Take is appropriate for weighted domain work; Allow costs one unit.
type Limiter[K any] struct {
	store      *Store
	definition *definition[K]
}

func (l Limiter[K]) Name() Name   { return (Declaration[K]{l.definition}).Name() }
func (l Limiter[K]) Limit() Limit { return (Declaration[K]{l.definition}).Limit() }
func (l Limiter[K]) Validate() error {
	if l.store == nil || l.store.backend == nil {
		return fault.New(fault.Invalid, "rate limiter is not bound")
	}
	return (Declaration[K]{l.definition}).Validate()
}
func (l Limiter[K]) Allow(ctx context.Context, key K) (Decision, error) { return l.Take(ctx, key, 1) }
func (l Limiter[K]) Take(ctx context.Context, key K, cost uint32) (Decision, error) {
	return l.TakeWith(ctx, cost, func(context.Context) (K, error) { return key, nil })
}

// TakeWith resolves a concrete key inside the same bounded operation as encoding
// and admission. It catches panic/Goexit and waits for callback exit. The resolver
// must honor cancellation; no callback or uncertain command is automatically retried.
func (l Limiter[K]) TakeWith(ctx context.Context, cost uint32, resolve func(context.Context) (K, error)) (Decision, error) {
	if err := l.validateCost(cost); err != nil {
		return Decision{}, err
	}
	var decision Decision
	err := l.run(ctx, resolve, func(ctx context.Context, address Key) error {
		var err error
		decision, err = l.store.backend.RateLimit(ctx, address, l.Limit(), cost)
		if err != nil {
			return err
		}
		return decision.Validate(l.Limit(), cost)
	})
	if err != nil {
		return Decision{}, err
	}
	return decision, nil
}

// Attempt takes cost and runs fn only when admitted, returning fn's error. A
// denial returns the decision without calling fn; an admission error returns
// it without calling fn. fn runs in the caller's goroutine after the bounded
// admission operation has finished, with the caller's context. Consumed capacity
// is not refunded when fn fails, and nothing is retried.
func (l Limiter[K]) Attempt(ctx context.Context, key K, cost uint32, fn func(context.Context) error) (Decision, error) {
	if fn == nil {
		return Decision{}, fault.New(fault.Invalid, "rate limit attempt requires a callback")
	}
	decision, err := l.Take(ctx, key, cost)
	if err != nil || !decision.Allowed {
		return decision, err
	}
	return decision, fn(ctx)
}

// Peek reports the decision cost would receive now without consuming capacity
// or changing the bucket's expiry. Remaining is the capacity left now. It is an
// observation, not a reservation: a later Take can be denied. The backend must
// implement InspectBackend; otherwise Peek fails with fault.Invalid.
func (l Limiter[K]) Peek(ctx context.Context, key K, cost uint32) (Decision, error) {
	if err := l.validateCost(cost); err != nil {
		return Decision{}, err
	}
	var decision Decision
	err := l.inspect(ctx, key, func(ctx context.Context, backend InspectBackend, address Key) error {
		var err error
		decision, err = backend.PeekRateLimit(ctx, address, l.Limit(), cost)
		if err != nil {
			return err
		}
		return decision.ValidatePeek(l.Limit(), cost)
	})
	if err != nil {
		return Decision{}, err
	}
	return decision, nil
}

// Remaining reports the capacity left in the key's current window without
// consuming it. A missing or expired bucket reports the full capacity.
func (l Limiter[K]) Remaining(ctx context.Context, key K) (uint32, error) {
	decision, err := l.Peek(ctx, key, 1)
	if err != nil {
		return 0, err
	}
	return decision.Remaining, nil
}

// AvailableIn reports how long until cost could be admitted: zero when it fits
// now, otherwise the time until the current window resets. It consumes nothing.
func (l Limiter[K]) AvailableIn(ctx context.Context, key K, cost uint32) (time.Duration, error) {
	decision, err := l.Peek(ctx, key, cost)
	if err != nil {
		return 0, err
	}
	return decision.RetryAfter, nil
}

// Clear removes the key's bucket so its next request starts a fresh window with
// full capacity. True means stored state existed. It also removes unreadable
// state at the key's address. Clearing grants new quota; use it deliberately
// (for example after a successful verification), never as error recovery.
func (l Limiter[K]) Clear(ctx context.Context, key K) (bool, error) {
	var cleared bool
	err := l.inspect(ctx, key, func(ctx context.Context, backend InspectBackend, address Key) error {
		var err error
		cleared, err = backend.ClearRateLimit(ctx, address)
		return err
	})
	if err != nil {
		return false, err
	}
	return cleared, nil
}

func (l Limiter[K]) validateCost(cost uint32) error {
	if err := l.Validate(); err != nil {
		return err
	}
	return l.Limit().ValidateCost(cost)
}
func (l Limiter[K]) inspect(ctx context.Context, key K, fn func(context.Context, InspectBackend, Key) error) error {
	if err := l.Validate(); err != nil {
		return err
	}
	backend, ok := l.store.backend.(InspectBackend)
	if !ok {
		return fault.New(fault.Invalid, "rate limit backend does not support inspection")
	}
	return l.run(ctx, func(context.Context) (K, error) { return key, nil }, func(ctx context.Context, address Key) error {
		return fn(ctx, backend, address)
	})
}

// run owns one bounded operation: deadline, queued admission, key resolution,
// encoding and the adapter call all execute inside one isolated callback that
// retains its slot until it actually exits.
func (l Limiter[K]) run(ctx context.Context, resolve func(context.Context) (K, error), fn func(context.Context, Key) error) error {
	if err := l.Validate(); err != nil {
		return err
	}
	if ctx == nil || resolve == nil {
		return fault.New(fault.Invalid, "rate limiting requires context and key resolver")
	}
	operation, cancel := context.WithTimeout(ctx, l.store.config.Timeout)
	defer cancel()
	if err := operation.Err(); err != nil {
		return err
	}
	if err := l.store.slots.Acquire(operation, admission.Wait(l.store.config.Timeout), nil); err != nil {
		return err
	}
	defer l.store.slots.Release()
	err := callback.Isolated("rate limit operation", func() error {
		if err := operation.Err(); err != nil {
			return err
		}
		key, err := resolve(operation)
		if err != nil {
			return err
		}
		if err := operation.Err(); err != nil {
			return err
		}
		logical, err := l.definition.codec.Encode(key)
		if err != nil {
			return err
		}
		if len(logical) > l.store.config.MaxKeyBytes {
			return fault.New(fault.Invalid, "encoded rate limit key exceeds its limit")
		}
		address, err := NewKey(l.store.config.Namespace, l.Name(), logical)
		if err != nil {
			return err
		}
		if err := operation.Err(); err != nil {
			return err
		}
		return fn(operation, address)
	})
	if err != nil {
		return err
	}
	return operation.Err()
}
