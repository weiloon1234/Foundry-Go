package pubsub

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

// MaxSuperviseBackoff bounds the delay between re-subscription attempts.
const MaxSuperviseBackoff = 10 * time.Minute

// SupervisePolicy bounds automatic re-subscription after an interruption. The
// delay starts at InitialBackoff, doubles (with jitter) after each failed
// attempt up to MaxBackoff, and resets once delivery has resumed.
type SupervisePolicy struct {
	InitialBackoff, MaxBackoff time.Duration
}

func DefaultSupervisePolicy() SupervisePolicy {
	return SupervisePolicy{InitialBackoff: 100 * time.Millisecond, MaxBackoff: 30 * time.Second}
}
func (p SupervisePolicy) Validate() error {
	if p.InitialBackoff <= 0 || p.MaxBackoff < p.InitialBackoff || p.MaxBackoff > MaxSuperviseBackoff {
		return fault.New(fault.Invalid, "invalid pub/sub supervision backoff")
	}
	return nil
}

// Gap reports one interruption after which delivery has resumed. Payloads
// published between Cause and the new subscription's readiness were lost:
// Redis pub/sub is at-most-once. Attempts counts failed re-subscriptions.
type Gap struct {
	Cause    error
	Attempts int
}

// Supervise keeps one key subscribed until ctx ends, the broker closes, or a
// callback returns an error. receive handles each payload in order. After any
// terminal interruption (disconnect, overflow, undecodable payload) Supervise
// re-subscribes with backoff and, once the NEW subscription is confirmed ready,
// calls gap before delivering anything else, so the application can reconcile
// state from its source of truth without missing a later change. Changes made
// while gap runs are queued (within Buffer limits) and delivered afterwards, so
// receive must tolerate a payload that the reconciliation already reflected.
// Invalid keys, cycles and contained callback panics end supervision instead of
// retrying; every ErrDisconnected (including one during establishment) is
// retried. It returns ctx.Err() on cancellation and ErrClosed on shutdown, the
// only permanent adapter failure.
func (t Topic[K, V]) Supervise(ctx context.Context, key K, policy SupervisePolicy, receive func(context.Context, V) error, gap func(context.Context, Gap) error) error {
	if err := t.Validate(); err != nil {
		return err
	}
	if ctx == nil || receive == nil || gap == nil {
		return fault.New(fault.Invalid, "pub/sub supervision needs a context, receiver and gap handler")
	}
	if err := policy.Validate(); err != nil {
		return err
	}
	var interruption error
	attempts := 0
	delay := policy.InitialBackoff
	for {
		subscription, err := t.Subscribe(ctx, key)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !resumable(err) {
				return err
			}
			if interruption == nil {
				interruption = err
			}
			attempts++
			if err := pause(ctx, &delay, policy); err != nil {
				return err
			}
			continue
		}
		err = t.supervised(ctx, subscription, &interruption, &attempts, receive, gap)
		if err == nil || ctx.Err() != nil {
			return ctx.Err()
		}
		if !interrupted(err) {
			return err
		}
		interruption, delay = err, policy.InitialBackoff
		if err := pause(ctx, &delay, policy); err != nil {
			return err
		}
	}
}

// supervised owns one subscription until it ends. A returned error that
// interrupted reports as true is a gap; any other error ends supervision.
func (t Topic[K, V]) supervised(ctx context.Context, subscription *Subscription[V], interruption *error, attempts *int, receive func(context.Context, V) error, gap func(context.Context, Gap) error) (err error) {
	defer func() {
		// A closing subscription's cleanup error does not hide the outcome.
		_ = subscription.Close(context.WithoutCancel(ctx))
	}()
	if *interruption != nil {
		report := Gap{Cause: *interruption, Attempts: *attempts}
		*interruption, *attempts = nil, 0
		if err := callback.Invoke("pub/sub gap handler", func() error { return gap(ctx, report) }); err != nil {
			return handlerFailure{err}
		}
	}
	for {
		value, err := subscription.Receive(ctx)
		if err != nil {
			return err
		}
		if err := callback.Invoke("pub/sub supervised receiver", func() error { return receive(ctx, value) }); err != nil {
			return handlerFailure{err}
		}
	}
}

// handlerFailure marks an application callback error, which is never a gap.
type handlerFailure struct{ cause error }

func (e handlerFailure) Error() string { return "pub/sub supervised handler failed" }
func (e handlerFailure) Unwrap() error { return e.cause }

// interrupted reports whether a subscription ended by losing delivery, as
// opposed to shutdown or an application handler failure.
func interrupted(err error) bool {
	if _, handler := err.(handlerFailure); handler {
		return false
	}
	closed := true
	failed := callback.Isolated("classify pub/sub interruption", func() error {
		closed = errorgraph.Is(err, ErrClosed)
		return nil
	})
	return failed == nil && !closed
}

// resumable reports whether a failed subscription attempt may succeed later.
// Only the explicit shutdown sentinel ErrClosed (broker or adapter closing) and
// caller-side failures (invalid key, duplicate, cycle, panic) are permanent. A
// disconnect is always retried, including one caused by a protocol anomaly or a
// connection that closed during establishment.
func resumable(err error) bool {
	permanent := true
	failed := callback.Isolated("classify pub/sub subscription failure", func() error {
		switch {
		case errorgraph.Is(err, ErrClosed):
			permanent = true
		case errorgraph.Is(err, ErrDisconnected):
			permanent = false
		default:
			permanent = false
			for _, code := range []fault.Code{fault.Invalid, fault.Duplicate, fault.Cycle, fault.Panicked} {
				if errorgraph.Is(err, code) {
					permanent = true
				}
			}
		}
		return nil
	})
	return failed == nil && !permanent
}

// pause waits the current jittered delay and doubles it for the next attempt.
func pause(ctx context.Context, delay *time.Duration, policy SupervisePolicy) error {
	wait := *delay/2 + time.Duration(rand.Int64N(int64(*delay-*delay/2)+1))
	*delay = min(*delay*2, policy.MaxBackoff)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
