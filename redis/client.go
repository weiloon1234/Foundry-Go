package redis

import (
	"context"
	"errors"
	"sync"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/pubsub"
)

// Client owns its connections and bounded in-flight operations. A cache.Store
// borrows this client; it does not close it. Use Module for application ownership.
type Client struct {
	config        Config
	mu            sync.Mutex
	raw           *driver.Client
	started       bool
	ready         bool
	closing       bool
	active        int
	startDone     chan struct{}
	startErr      error
	drained       chan struct{}
	done          chan struct{}
	closeErr      error
	subscriptions map[*subscription]struct{}
}

// Prepare snapshots validated configuration without I/O or background goroutines.
func Prepare(config Config) (*Client, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Client{config: config.snapshot(), startDone: make(chan struct{}), drained: make(chan struct{}), done: make(chan struct{}), subscriptions: make(map[*subscription]struct{})}, nil
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
	c.started = true
	c.active++
	c.mu.Unlock()
	raw := driver.NewClient(c.config.options())
	c.mu.Lock()
	c.raw = raw
	c.mu.Unlock()
	attempt, cancel := context.WithTimeout(ctx, c.config.ConnectTimeout)
	err := classify(attempt, raw.Ping(attempt).Err())
	cancel()
	if err != nil {
		err = errors.Join(err, classify(context.Background(), raw.Close()))
		c.mu.Lock()
		c.raw = nil
		c.mu.Unlock()
	}
	c.mu.Lock()
	if err == nil && c.closing {
		err = fault.New(fault.Closed, "Redis client closed during startup")
	}
	c.startErr = err
	c.ready = err == nil
	close(c.startDone)
	c.mu.Unlock()
	c.release()
	return err
}

// Ping verifies the running client under the same operation bounds as features.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.execute(ctx, func(ctx context.Context, raw *driver.Client) (any, error) { return nil, raw.Ping(ctx).Err() })
	return err
}
func (c *Client) execute(ctx context.Context, operation func(context.Context, *driver.Client) (any, error)) (any, error) {
	if err := c.valid(ctx); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	switch {
	case c.closing:
		c.mu.Unlock()
		return nil, fault.New(fault.Closed, "Redis client is closed")
	case !c.ready:
		c.mu.Unlock()
		return nil, fault.New(fault.Conflict, "Redis client has not started successfully")
	case c.active >= c.config.MaxOperations:
		c.mu.Unlock()
		return nil, fault.New(fault.Conflict, "Redis operation capacity reached")
	}
	c.active++
	raw := c.raw
	c.mu.Unlock()
	defer c.release()
	operationCtx, cancel := context.WithTimeout(ctx, c.config.OperationTimeout)
	defer cancel()
	if err := operationCtx.Err(); err != nil {
		return nil, err
	}
	value, err := operation(operationCtx, raw)
	return value, classify(operationCtx, err)
}
func (c *Client) release() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.active--
	if c.closing && c.active == 0 && len(c.subscriptions) == 0 {
		close(c.drained)
	}
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
		for sub := range c.subscriptions {
			sub.stop(pubsub.ErrClosed)
		}
		if c.active == 0 && len(c.subscriptions) == 0 {
			close(c.drained)
		}
		go func() {
			<-c.drained
			if c.raw != nil {
				c.closeErr = classify(context.Background(), c.raw.Close())
			}
			close(c.done)
		}()
	}
	c.mu.Unlock()
	select {
	case <-c.done:
		return c.closeErr
	default:
	}
	select {
	case <-c.done:
		return c.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
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
	defer c.mu.Unlock()
	value := Stats{Operations: c.active, Subscriptions: len(c.subscriptions), Ready: c.ready && !c.closing, Closing: c.closing}
	if c.raw != nil {
		pool := c.raw.PoolStats()
		value.Open = int(pool.TotalConns)
		value.Idle = int(pool.IdleConns)
		value.SubscriptionConnections = int(pool.PubSubStats.Active)
	}
	return value
}
