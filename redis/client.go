package redis

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkadapter"
	"github.com/weiloon1234/Foundry-Go/pubsub"
)

// closingBit marks shutdown in Client.owners; the low bits count owned work.
const closingBit = int64(1) << 62

// Client owns its connections and bounded in-flight operations. A cache.Store
// borrows this client; it does not close it. Use Module for application ownership.
//
// The per-operation path uses atomics and a FIFO admission semaphore only; the
// mutex guards lifecycle transitions and the subscription registry.
type Client struct {
	config Config
	// owners counts every admitted operation, admission wait and live
	// subscription, plus closingBit once Close begins. Close drains it to zero.
	owners     atomic.Int64
	operations atomic.Int64
	slots      *admission.Semaphore
	// ready publishes raw: Start writes raw under mu before storing ready, and
	// operations read raw only after observing ready (or after draining).
	ready atomic.Bool
	raw   *driver.Client

	mu            sync.Mutex
	started       bool
	closing       bool
	startDone     chan struct{}
	startErr      error
	stopping      chan struct{}
	drainOnce     sync.Once
	drained       chan struct{}
	done          chan struct{}
	closeErr      error
	subscriptions map[*subscription]struct{}
	// hub is the shared subscription connection (see pubsub.go).
	hub *hub
}

// FoundryAdapter marks the client as framework-owned adapter I/O.
func (*Client) FoundryAdapter(frameworkadapter.Seal) {}

// Prepare snapshots validated configuration without I/O or background goroutines.
func Prepare(config Config) (*Client, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Client{config: config.snapshot(), slots: admission.New(config.MaxOperations), startDone: make(chan struct{}), stopping: make(chan struct{}), drained: make(chan struct{}), done: make(chan struct{}), subscriptions: make(map[*subscription]struct{})}, nil
}

// Open prepares and starts a client, closing all owned resources on startup error.
func Open(ctx context.Context, config Config) (*Client, error) {
	client, err := Prepare(config)
	if err != nil {
		return nil, err
	}
	if err = client.Start(ctx); err != nil {
		return nil, errors.Join(err, client.Close(context.Background()))
	}
	return client, nil
}
func (c *Client) valid(ctx context.Context) error {
	if c == nil || c.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "Redis needs an initialized client and context")
	}
	return nil
}

// enter registers owned work unless shutdown has begun.
func (c *Client) enter() bool {
	for {
		state := c.owners.Load()
		if state&closingBit != 0 {
			return false
		}
		if c.owners.CompareAndSwap(state, state+1) {
			return true
		}
	}
}

// leave releases one owner and completes the drain after the last one.
func (c *Client) leave() {
	if c.owners.Add(-1) == closingBit {
		c.drainOnce.Do(func() { close(c.drained) })
	}
}

// Start checks connectivity once. The first caller owns that attempt; concurrent
// callers can cancel their wait. A failed start is terminal for this instance.
func (c *Client) Start(ctx context.Context) error {
	if err := c.valid(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	if c.closing {
		c.mu.Unlock()
		return fault.New(fault.Closed, "Redis client is closed")
	}
	if c.started {
		c.mu.Unlock()
		select {
		case <-c.startDone:
			return c.startErr
		default:
		}
		select {
		case <-c.startDone:
			return c.startErr
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		c.mu.Unlock()
		return err
	}
	if !c.enter() {
		c.mu.Unlock()
		return fault.New(fault.Closed, "Redis client is closed")
	}
	c.started = true
	c.mu.Unlock()
	defer c.leave()
	raw := driver.NewClient(c.config.options())
	c.mu.Lock()
	c.raw = raw
	c.mu.Unlock()
	attempt, cancel := context.WithTimeout(ctx, c.config.ConnectTimeout)
	err := classify(attempt, raw.Ping(attempt).Err())
	cancel()
	if err != nil {
		err = errors.Join(err, classify(context.Background(), raw.Close()))
	}
	c.mu.Lock()
	if err != nil {
		c.raw = nil
	}
	if err == nil && c.closing {
		err = fault.New(fault.Closed, "Redis client closed during startup")
	}
	c.startErr = err
	c.ready.Store(err == nil)
	close(c.startDone)
	c.mu.Unlock()
	return err
}

// Ping verifies the running client under the same operation bounds as features.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.execute(ctx, func(ctx context.Context, raw *driver.Client) (any, error) { return nil, raw.Ping(ctx).Err() })
	return err
}

// execute admits one bounded command. A full MaxOperations bound queues in FIFO
// order for at most admission.Wait(OperationTimeout) and then returns
// fault.Overloaded; Close stops waiting callers with fault.Closed.
func (c *Client) execute(ctx context.Context, operation func(context.Context, *driver.Client) (any, error)) (any, error) {
	if err := c.valid(ctx); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !c.enter() {
		return nil, fault.New(fault.Closed, "Redis client is closed")
	}
	defer c.leave()
	if !c.ready.Load() {
		return nil, fault.New(fault.Conflict, "Redis client has not started successfully")
	}
	if err := c.slots.Acquire(ctx, admission.Wait(c.config.OperationTimeout), c.stopping); err != nil {
		return nil, err
	}
	defer c.slots.Release()
	c.operations.Add(1)
	defer c.operations.Add(-1)
	operationCtx, cancel := context.WithTimeout(ctx, c.config.OperationTimeout)
	defer cancel()
	if err := operationCtx.Err(); err != nil {
		return nil, err
	}
	value, err := operation(operationCtx, c.raw)
	return value, classify(operationCtx, err)
}

// Close rejects new work and drains owned operations before closing connections.
// Caller cancellation only stops waiting; cleanup continues. In-flight commands
// retain their own bounded contexts. Close is safe to repeat concurrently.
func (c *Client) Close(ctx context.Context) error {
	if err := c.valid(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	if !c.closing {
		c.closing = true
		close(c.stopping)
		previous := c.owners.Or(closingBit)
		for sub := range c.subscriptions {
			sub.stop(pubsub.ErrClosed)
		}
		if previous == 0 {
			c.drainOnce.Do(func() { close(c.drained) })
		}
		go func() {
			<-c.drained
			c.mu.Lock()
			raw := c.raw
			c.mu.Unlock()
			var err error
			if raw != nil {
				err = classify(context.Background(), raw.Close())
			}
			c.mu.Lock()
			c.closeErr = err
			c.mu.Unlock()
			close(c.done)
		}()
	}
	c.mu.Unlock()
	select {
	case <-c.done:
		return c.closeError()
	default:
	}
	select {
	case <-c.done:
		return c.closeError()
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (c *Client) closeError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeErr
}

// Done closes after all operations have drained and connections have closed.
// The zero client has no lifecycle and returns nil.
func (c *Client) Done() <-chan struct{} {
	if c == nil {
		return nil
	}
	return c.done
}

// Stats is a concurrent snapshot of owned operations and connection use.
type Stats struct {
	Open, Idle, Operations int
	// Subscriptions includes establishment/drain; SubscriptionConnections is the
	// driver's live dedicated connection count, separate from Open.
	Subscriptions, SubscriptionConnections int
	Ready, Closing                         bool
}

func (c *Client) Stats() Stats {
	if c == nil || c.done == nil {
		return Stats{Closing: true}
	}
	c.mu.Lock()
	value := Stats{Operations: int(c.operations.Load()), Subscriptions: len(c.subscriptions), Ready: c.ready.Load() && !c.closing, Closing: c.closing}
	raw := c.raw
	c.mu.Unlock()
	if raw != nil {
		pool := raw.PoolStats()
		value.Open = int(pool.TotalConns)
		value.Idle = int(pool.IdleConns)
		value.SubscriptionConnections = int(pool.PubSubStats.Active)
	}
	return value
}
