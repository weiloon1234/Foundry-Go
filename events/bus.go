package events

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/contextlink"
)

// Config bounds concurrent captures/deliveries and the active dispatch chain.
// Admission never waits for another dispatch, including recursive calls.
type Config struct {
	MaxInFlight int
	MaxDepth    int
}

func DefaultConfig() Config { return Config{MaxInFlight: 64, MaxDepth: 32} }
func (c Config) Validate() error {
	if c.MaxInFlight <= 0 || c.MaxDepth <= 0 {
		return fault.New(fault.Invalid, "event concurrency and dispatch depth must be positive")
	}
	return nil
}

// Bus owns an immutable registry and synchronous dispatch lifetimes. Prepare is
// pure construction; Start connects its application lifetime. Close cancels work
// and waits for actual handler exit, even when a handler ignores cancellation.
// A Bus must not be copied. Its zero value is invalid; use Prepare or Module.
type Bus struct {
	mu           sync.Mutex
	config       Config
	registry     registry
	bound        bool
	managed      bool
	started      bool
	closing      bool
	active       int
	lifetime     context.Context
	cancel       context.CancelFunc
	stopLifetime func() bool
	done         chan struct{}
}

func Prepare(config Config, declarations ...Declaration) (*Bus, error) {
	bus, err := prepare(config)
	if err != nil {
		return nil, err
	}
	if err := bus.bind(declarations, false); err != nil {
		return nil, err
	}
	return bus, nil
}
func prepare(config Config) (*Bus, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Bus{config: config, done: make(chan struct{})}, nil
}
func (b *Bus) bind(declarations []Declaration, managed bool) error {
	if b == nil {
		return fault.New(fault.Invalid, "nil event bus")
	}
	registry, err := newRegistry(declarations)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bound || b.started || b.closing {
		return fault.New(fault.Closed, "event registration is frozen")
	}
	if b.managed != managed {
		return fault.New(fault.Invalid, "application module owns event registration")
	}
	b.registry = registry
	b.bound = true
	return nil
}

func (b *Bus) Start(ctx context.Context) error { return b.start(ctx, false) }
func (b *Bus) start(ctx context.Context, managed bool) error {
	if b == nil || ctx == nil {
		return fault.New(fault.Invalid, "event startup requires a bus and context")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.managed != managed {
		return fault.New(fault.Invalid, "application module owns event startup")
	}
	if b.closing {
		return fault.New(fault.Closed, "event bus is closing")
	}
	if !b.bound {
		return fault.New(fault.Invalid, "event registration is not bound")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.started {
		if b.lifetime.Err() != nil {
			return fault.New(fault.Closed, "event lifetime ended")
		}
		return nil
	}
	b.started = true
	b.lifetime, b.cancel = context.WithCancel(ctx)
	b.stopLifetime = context.AfterFunc(b.lifetime, func() { b.beginClose() })
	return nil
}

// Done closes only after admission has stopped and every owned callback exited.
func (b *Bus) Done() <-chan struct{} { return b.done }

// Close starts shutdown even if the supplied wait context has expired. A timed
// out caller can wait again; callbacks remain owned until they actually return.
// Closing from this bus's active handler context fails instead of waiting on itself.
func (b *Bus) Close(ctx context.Context) error {
	if b == nil || ctx == nil || b.done == nil {
		return fault.New(fault.Invalid, "event shutdown requires a bus and context")
	}
	for _, frame := range chainFrom(ctx) {
		if frame.bus == b && frame.active.Load() {
			return fault.New(fault.Cycle, "event handler cannot wait for its own bus shutdown")
		}
	}
	b.beginClose()
	select {
	case <-b.done:
		return nil
	default:
	}
	select {
	case <-b.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (b *Bus) beginClose() {
	b.mu.Lock()
	if b.closing {
		b.mu.Unlock()
		return
	}
	b.closing = true
	cancel, stop := b.cancel, b.stopLifetime
	if b.active == 0 {
		close(b.done)
	}
	b.mu.Unlock()
	if stop != nil {
		stop()
	}
	if cancel != nil {
		cancel()
	}
}

type dispatchFrame struct {
	bus    *Bus
	active atomic.Bool
}
type dispatchChainKey struct{}

func chainFrom(ctx context.Context) []*dispatchFrame {
	chain, _ := ctx.Value(dispatchChainKey{}).([]*dispatchFrame)
	return chain
}

func (b *Bus) begin(ctx context.Context, key topicKey, typ reflect.Type) (context.Context, registration, func(), error) {
	if b == nil || ctx == nil {
		return nil, registration{}, nil, fault.New(fault.Invalid, "event dispatch requires a bus and context")
	}
	if err := ctx.Err(); err != nil {
		return nil, registration{}, nil, err
	}
	// Retain only active ancestors; a saved context must not accumulate completed
	// operations or falsely identify them as current recursive dispatches.
	chain := make([]*dispatchFrame, 0)
	for _, frame := range chainFrom(ctx) {
		if frame.active.Load() {
			chain = append(chain, frame)
		}
	}
	b.mu.Lock()
	if !b.started || b.closing || b.lifetime == nil || b.lifetime.Err() != nil {
		b.mu.Unlock()
		return nil, registration{}, nil, fault.New(fault.Closed, "event bus is not running")
	}
	entry, err := b.registry.lookup(key, typ)
	if err != nil {
		b.mu.Unlock()
		return nil, registration{}, nil, err
	}
	if len(chain) >= b.config.MaxDepth {
		b.mu.Unlock()
		return nil, registration{}, nil, fault.New(fault.Cycle, "event dispatch depth exceeded")
	}
	if b.active >= b.config.MaxInFlight {
		b.mu.Unlock()
		return nil, registration{}, nil, fault.New(fault.Conflict, "event dispatch capacity is exhausted")
	}
	b.active++
	lifetime := b.lifetime
	b.mu.Unlock()
	frame := &dispatchFrame{bus: b}
	frame.active.Store(true)
	operation, cancel := contextlink.Link(ctx, lifetime)
	operation = context.WithValue(operation, dispatchChainKey{}, append(chain, frame))
	release := func() {
		frame.active.Store(false)
		cancel()
		b.mu.Lock()
		defer b.mu.Unlock()
		b.active--
		if b.closing && b.active == 0 {
			close(b.done)
		}
	}
	return operation, entry, release, nil
}
