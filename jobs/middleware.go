package jobs

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Middleware surrounds a concrete handler. Before runs in registration order;
// After and Failed unwind entered middleware in reverse order.
// Every callback is owned until it exits and panic/Goexit become ordinary errors.
// Failed also runs when After fails. Errors join without embedding them in history.
type Middleware[P any] struct {
	Before Handler[P]
	After  Handler[P]
	Failed func(context.Context, P, error) error
}

// Admission returns zero to run or a positive delay to release the reservation
// without consuming an attempt. It must honor context. Admission errors consume
// attempts and use the ordinary retry policy. Invalid payloads fail permanently.
type Admission[P any] func(context.Context, P) (time.Duration, error)
type HandlerOptions[P any] struct {
	Middleware []Middleware[P]
	Admission  Admission[P]
}

// RateLimit reuses a typed shared limiter. Each admitted attempt costs one unit;
// denials delay work without consuming its retry budget. Unknown limiter results
// are errors, never permission to execute. Quotas are scoped by the supplied
// limiter and key resolver, so applications can share one quota across job types.
func RateLimit[P, K any](limiter ratelimit.Limiter[K], key func(P) K) Admission[P] {
	return func(ctx context.Context, payload P) (time.Duration, error) {
		if key == nil {
			return 0, fault.New(fault.Invalid, "job rate limit requires a key resolver")
		}
		decision, err := limiter.TakeWith(ctx, 1, func(context.Context) (K, error) { return key(payload), nil })
		if err != nil {
			return 0, err
		}
		if !decision.Allowed {
			return decision.RetryAfter, nil
		}
		return 0, nil
	}
}

// DeclareWith binds typed middleware and admission without serializing services.
// Construct declarations once during application assembly. Producer-only
// declarations cannot attach execution callbacks.
func (d Definition[P]) DeclareWith(handler Handler[P], options HandlerOptions[P]) (Declaration, error) {
	if err := d.Validate(); err != nil {
		return Declaration{}, err
	}
	if len(options.Middleware) > 64 {
		return Declaration{}, fault.New(fault.Invalid, "job middleware capacity exceeded")
	}
	if handler == nil && (len(options.Middleware) > 0 || options.Admission != nil) {
		return Declaration{}, fault.New(fault.Invalid, "producer-only job cannot have execution callbacks")
	}
	declaration := d.declaration()
	middleware := slices.Clone(options.Middleware)
	if handler == nil {
		return declaration, nil
	}
	declaration.prepare = func(ctx context.Context, text string) (func(context.Context) error, time.Duration, error) {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		snapshot, err := value.ParseJSON[P](text)
		if err != nil {
			return nil, 0, &payloadError{cause: err}
		}
		payload, err := snapshot.Decode()
		if err != nil {
			return nil, 0, &payloadError{cause: err}
		}
		if options.Admission != nil {
			delay, err := options.Admission(ctx, payload)
			if err != nil {
				return nil, 0, err
			}
			if delay < 0 || delay > MaxDelay {
				return nil, 0, fault.New(fault.Invalid, "job admission returned an invalid delay")
			}
			if delay > 0 {
				return nil, delay, nil
			}
		}
		return func(ctx context.Context) error { return invokeMiddleware(ctx, payload, handler, middleware) }, 0, nil
	}
	return declaration, nil
}
func invokeMiddleware[P any](ctx context.Context, payload P, handler Handler[P], middleware []Middleware[P]) error {
	var result error
	entered := 0
	for i, item := range middleware {
		entered = i + 1
		if err := ctx.Err(); err != nil {
			result = err
			break
		}
		if item.Before != nil {
			result = callback.Isolated("before job", func() error { return item.Before(ctx, payload) })
		}
		if result != nil {
			break
		}
	}
	if result == nil {
		if err := ctx.Err(); err != nil {
			result = err
		} else {
			result = callback.Isolated("job handler", func() error { return handler(ctx, payload) })
		}
	}
	if result == nil {
		for i := entered - 1; i >= 0; i-- {
			item := middleware[i]
			if item.After != nil {
				result = errors.Join(result, callback.Isolated("after job", func() error { return item.After(ctx, payload) }))
			}
		}
	}
	if result != nil {
		for i := entered - 1; i >= 0; i-- {
			item := middleware[i]
			if item.Failed != nil {
				failure := result
				result = errors.Join(result, callback.Isolated("failed job", func() error { return item.Failed(ctx, payload, failure) }))
			}
		}
	}
	return result
}
