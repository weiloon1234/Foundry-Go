package ratelimit

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/fault"
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
	if err := l.Validate(); err != nil {
		return Decision{}, err
	}
	if ctx == nil || resolve == nil {
		return Decision{}, fault.New(fault.Invalid, "rate limiting requires context and key resolver")
	}
	if err := l.Limit().ValidateCost(cost); err != nil {
		return Decision{}, err
	}
	operation, cancel := context.WithTimeout(ctx, l.store.config.Timeout)
	defer cancel()
	if err := operation.Err(); err != nil {
		return Decision{}, err
	}
	select {
	case l.store.slots <- struct{}{}:
	default:
		return Decision{}, fault.New(fault.Conflict, "rate limit operation capacity reached")
	}
	defer func() { <-l.store.slots }()
	var decision Decision
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
		decision, err = l.store.backend.RateLimit(operation, address, l.Limit(), cost)
		if err != nil {
			return err
		}
		return decision.Validate(l.Limit(), cost)
	})
	if err != nil {
		return Decision{}, err
	}
	if err := operation.Err(); err != nil {
		return Decision{}, err
	}
	return decision, nil
}
