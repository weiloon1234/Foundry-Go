package pubsub

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/contextlink"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

// Broker borrows its adapter and owns callbacks and typed subscriptions. Pure
// construction performs no I/O. Close cancels work and waits for actual exit.
type Broker struct {
	backend       Backend
	config        Config
	slots         *admission.Semaphore
	mu            sync.Mutex
	closing       bool
	active        int
	declarations  map[declarationKey]*declarationID
	subscriptions map[*subscriptionState]struct{}
	lifetime      context.Context
	cancel        context.CancelFunc
	done          chan struct{}
	closeErr      error
}

func NewBroker(backend Backend, config Config) (*Broker, error) {
	if backend == nil {
		return nil, fault.New(fault.Invalid, "pub/sub broker requires an adapter")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithCancel(context.Background())
	return &Broker{backend: backend, config: config, slots: admission.New(config.MaxConcurrent), declarations: make(map[declarationKey]*declarationID), subscriptions: make(map[*subscriptionState]struct{}), lifetime: lifetime, cancel: cancel, done: make(chan struct{})}, nil
}
func (b *Broker) Namespace() keyspace.Namespace { return b.config.Namespace }
func (b *Broker) Done() <-chan struct{} {
	if b == nil {
		return nil
	}
	return b.done
}

type operationKey struct{}
type operationFrame struct {
	broker       *Broker
	subscription *subscriptionState
	parent       *operationFrame
	active       atomic.Bool
}

func (b *Broker) operation(ctx context.Context, sub *subscriptionState) (context.Context, func()) {
	parent, _ := ctx.Value(operationKey{}).(*operationFrame)
	frame := &operationFrame{broker: b, subscription: sub, parent: parent}
	frame.active.Store(true)
	parents := []context.Context{b.lifetime}
	if sub != nil {
		parents = append(parents, sub.lifetime)
	}
	linked, release := contextlink.Link(context.WithValue(ctx, operationKey{}, frame), parents...)
	return linked, func() { frame.active.Store(false); release() }
}

// acquire reserves one MaxConcurrent slot. A full broker queues in FIFO order
// for at most admission.Wait(Timeout) and the caller's deadline, then returns
// retryable fault.Overloaded; Close ends waits with ErrClosed. An operation
// nested inside one of this broker's own callbacks never waits, so it cannot
// deadlock on capacity held by its caller.
func (b *Broker) acquire(ctx context.Context) error {
	if b == nil || b.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "pub/sub operation needs an initialized broker and context")
	}
	b.mu.Lock()
	closing := b.closing
	b.mu.Unlock()
	if closing {
		return ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.nested(ctx) {
		if !b.slots.TryAcquire() {
			return fault.New(fault.Overloaded, "pub/sub operation capacity is exhausted")
		}
	} else if err := b.slots.Acquire(ctx, admission.Wait(b.config.Timeout), b.lifetime.Done()); err != nil {
		if errors.Is(err, fault.Closed) {
			return ErrClosed
		}
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closing {
		b.slots.Release()
		return ErrClosed
	}
	b.active++
	return nil
}

// nested reports whether ctx belongs to an active operation of this broker.
func (b *Broker) nested(ctx context.Context) bool {
	for frame, _ := ctx.Value(operationKey{}).(*operationFrame); frame != nil; frame = frame.parent {
		if frame.broker == b && frame.active.Load() {
			return true
		}
	}
	return false
}
func (b *Broker) release() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.active--
	b.slots.Release()
	b.finishLocked()
}
func (b *Broker) finishLocked() {
	if b.closing && b.active == 0 && len(b.subscriptions) == 0 {
		select {
		case <-b.done:
		default:
			close(b.done)
		}
	}
}
func (b *Broker) execute(ctx context.Context, fn func(context.Context) error) error {
	if err := b.acquire(ctx); err != nil {
		return err
	}
	defer b.release()
	ctx, release := b.operation(ctx, nil)
	defer release()
	ctx, cancel := context.WithTimeout(ctx, b.config.Timeout)
	defer cancel()
	err := callback.Isolated("pub/sub operation", func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return fn(ctx)
	})
	if err != nil {
		return err
	}
	return ctx.Err()
}
func (b *Broker) remove(s *subscriptionState, cleanup error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subscriptions, s)
	if b.closing {
		b.closeErr = errors.Join(b.closeErr, cleanup)
	}
	close(s.done)
	b.finishLocked()
}

// Close begins shutdown even when its wait context has expired. Canceled callers
// can wait again; no resource slot is released before the owned callback exits.
func (b *Broker) Close(ctx context.Context) error {
	if b == nil || b.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "pub/sub close needs a broker and context")
	}
	for frame, _ := ctx.Value(operationKey{}).(*operationFrame); frame != nil; frame = frame.parent {
		if frame.broker == b && frame.active.Load() {
			return fault.New(fault.Cycle, "pub/sub operation cannot wait for its own broker shutdown")
		}
	}
	b.mu.Lock()
	if !b.closing {
		b.closing = true
		b.cancel()
		for s := range b.subscriptions {
			s.beginClose(ErrClosed)
		}
		b.finishLocked()
	}
	b.mu.Unlock()
	select {
	case <-b.done:
		return b.closeErr
	default:
	}
	select {
	case <-b.done:
		return b.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stats includes setup, live and draining subscriptions until their resources and
// active callbacks have finished. Reading it performs no I/O.
type Stats struct {
	Operations, Subscriptions int
	Closing                   bool
}

func (b *Broker) Stats() Stats {
	if b == nil || b.done == nil {
		return Stats{Closing: true}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return Stats{Operations: b.active, Subscriptions: len(b.subscriptions), Closing: b.closing}
}
