package redis

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkadapter"
	"github.com/weiloon1234/Foundry-Go/internal/pubsubstream"
	"github.com/weiloon1234/Foundry-Go/pubsub"
)

var _ pubsub.Backend = (*Client)(nil)

// Publish attempts one ephemeral publication. The count is server subscriptions,
// not delivery/handler completion. Uncertain publications are never retried.
func (c *Client) Publish(ctx context.Context, channel pubsub.Channel, data []byte) (uint64, error) {
	if err := pubsub.ValidatePublish(ctx, channel, data); err != nil {
		return 0, err
	}
	if err := c.valid(ctx); err != nil {
		return 0, err
	}
	if len(data) > c.config.MaxValueBytes {
		return 0, fault.New(fault.Invalid, "Redis publication exceeds its payload limit")
	}
	result, err := c.execute(ctx, func(ctx context.Context, raw *driver.Client) (any, error) {
		return raw.Publish(ctx, channel.String(), data).Result()
	})
	if err != nil {
		return 0, err
	}
	count, ok := result.(int64)
	if !ok || count < 0 {
		return 0, fault.New(fault.Internal, "invalid Redis publication count")
	}
	return uint64(count), nil
}

// errHubClosing reports that registration raced the shared connection's shutdown
// (typically a failing connection). It is a retryable disconnect, not the
// client's own shutdown, which is reported as pubsub.ErrClosed.
var errHubClosing = fault.Wrap(fault.Conflict, "Redis subscription connection is closing", pubsub.ErrDisconnected)

// hubAttempts bounds how often establishment moves to a fresh shared connection
// after racing the previous connection's shutdown.
const hubAttempts = 2

// Subscribe confirms every exact channel before returning. All subscriptions of
// one Client share one dedicated connection with one reader and one heartbeat;
// a channel already confirmed on that connection is ready immediately, and a new
// channel waits for its own SUBSCRIBE acknowledgement. Establishment context
// cancellation never leaves an unowned reader. Any connection failure (receive
// error, heartbeat loss or unexpected protocol frame) terminates EVERY stream on
// that connection with pubsub.ErrDisconnected; vendor reconnect attempts never
// imply continuous delivery or cause automatic public resubscription.
func (c *Client) Subscribe(ctx context.Context, channels []pubsub.Channel, limits pubsub.Limits) (pubsub.Stream, error) {
	if err := pubsub.ValidateSubscribe(ctx, channels, limits); err != nil {
		return nil, err
	}
	if err := c.valid(ctx); err != nil {
		return nil, err
	}
	sub, err := c.reserveSubscription(ctx, channels, limits)
	if err != nil {
		return nil, err
	}
	defer c.releaseEstablishment()
	attempt, cancel := context.WithTimeout(ctx, c.config.OperationTimeout)
	defer cancel()
	for try := 1; ; try++ {
		err = sub.hub.register(sub)
		if !errors.Is(err, errHubClosing) || try == hubAttempts {
			break
		}
		if err = c.attachHub(sub); err != nil {
			break
		}
	}
	if err == nil {
		err = sub.await(attempt)
	}
	if err != nil {
		cause := classify(attempt, err)
		sub.stop(cause)
		<-sub.done
		return nil, errors.Join(cause, sub.cleanupErr)
	}
	return sub, nil
}

func (c *Client) reserveSubscription(ctx context.Context, channels []pubsub.Channel, limits pubsub.Limits) (*subscription, error) {
	if !c.enter() {
		return nil, pubsub.ErrClosed
	}
	if !c.ready.Load() {
		c.leave()
		return nil, fault.New(fault.Conflict, "Redis client has not started successfully")
	}
	// Establishment is an ordinary bounded operation: it waits for capacity.
	if err := c.slots.Acquire(ctx, admission.Wait(c.config.OperationTimeout), c.stopping); err != nil {
		c.leave()
		return nil, err
	}
	sub, err := c.registerSubscription(ctx, channels, limits)
	if err != nil {
		c.slots.Release()
		c.leave()
		return nil, err
	}
	return sub, nil
}

// registerSubscription records one live subscription owner and reserves a place
// on the shared connection. The caller keeps its establishment slot and owner
// until Subscribe returns (see releaseEstablishment).
func (c *Client) registerSubscription(ctx context.Context, channels []pubsub.Channel, limits pubsub.Limits) (*subscription, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing {
		return nil, pubsub.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Live subscriptions are a hard resource quota, not a queue: they are
	// long-lived, so waiting for one to close would not be bounded.
	if len(c.subscriptions) >= c.config.MaxSubscriptions {
		return nil, fault.New(fault.Overloaded, "Redis subscription capacity reached")
	}
	buffer, err := pubsubstream.New(limits)
	if err != nil {
		return nil, err
	}
	if !c.enter() {
		return nil, pubsub.ErrClosed
	}
	sub := &subscription{client: c, buffer: buffer, channels: make(map[string]pubsub.Channel, len(channels)), confirmed: make(map[string]bool, len(channels)), names: make([]string, len(channels)), ready: make(chan struct{}), done: make(chan struct{}), payloadLimit: limits.PayloadBytes}
	for i, channel := range channels {
		sub.names[i] = channel.String()
		sub.channels[sub.names[i]] = channel
	}
	sub.hub = c.currentHubLocked()
	c.operations.Add(1)
	c.subscriptions[sub] = struct{}{}
	return sub, nil
}

// attachHub reserves a place on a fresh shared connection after the previous
// one began shutting down during registration.
func (c *Client) attachHub(sub *subscription) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing {
		return pubsub.ErrClosed
	}
	sub.hub = c.currentHubLocked()
	return nil
}

// currentHubLocked returns the live shared connection, creating it when needed,
// and reserves one registration on it. Call with c.mu held and c not closing.
func (c *Client) currentHubLocked() *hub {
	if c.hub == nil || c.hub.closing.Load() {
		c.hub = newHub(c)
	}
	c.hub.mu.Lock()
	c.hub.pending++
	c.hub.mu.Unlock()
	return c.hub
}

// releaseEstablishment ends the Subscribe operation; the subscription keeps
// its own owner until its cleanup completes.
func (c *Client) releaseEstablishment() {
	c.operations.Add(-1)
	c.slots.Release()
	c.leave()
}

// subscription is one public stream multiplexed over its Client's hub. It owns
// no goroutine or connection; its bounded buffer receives only confirmed channels.
type subscription struct {
	client       *Client
	hub          *hub
	buffer       *pubsubstream.Buffer
	channels     map[string]pubsub.Channel
	names        []string
	payloadLimit int
	// confirmed and waiting are guarded by hub.mu; ready closes once every
	// channel has been acknowledged on the shared connection.
	confirmed  map[string]bool
	waiting    int
	ready      chan struct{}
	once       sync.Once
	done       chan struct{}
	cleanupErr error
}

// FoundryAdapter marks the stream as framework-owned adapter I/O.
func (*subscription) FoundryAdapter(frameworkadapter.Seal) {}

func (s *subscription) Next(ctx context.Context) (pubsub.Message, error) { return s.buffer.Next(ctx) }
func (s *subscription) Done() <-chan struct{}                            { return s.done }
func (s *subscription) Err() error                                       { return s.buffer.Err() }
func (s *subscription) Close(ctx context.Context) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "Redis subscription close needs context")
	}
	s.stop(pubsub.ErrClosed)
	select {
	case <-s.done:
		return s.cleanupErr
	default:
	}
	select {
	case <-s.done:
		return s.cleanupErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// await waits for every channel acknowledgement or for the stream to end.
func (s *subscription) await(ctx context.Context) error {
	select {
	case <-s.ready:
		return nil
	default:
	}
	select {
	case <-s.ready:
		return nil
	case <-s.buffer.Done():
		return s.buffer.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// stop terminates the stream once. Cleanup leaves the shared connection (which
// unsubscribes channels no other stream needs) and, when this was the last
// stream or the connection is failing, waits for the connection to close.
func (s *subscription) stop(reason error) {
	s.once.Do(func() {
		s.buffer.Finish(reason)
		go func() {
			// Establishment may move to a fresh connection; read it under mu.
			s.client.mu.Lock()
			h := s.hub
			s.client.mu.Unlock()
			if closed := h.unregister(s); closed != nil {
				<-closed
				s.cleanupErr = h.closeErr
			}
			s.client.mu.Lock()
			delete(s.client.subscriptions, s)
			close(s.done)
			s.client.mu.Unlock()
			s.client.leave()
		}()
	})
}

// deliver queues a confirmed message; a full or over-bound queue ends only
// this stream, explicitly, never silently dropping and continuing.
func (s *subscription) deliver(channel pubsub.Channel, payload []byte) {
	if len(payload) > s.payloadLimit {
		s.stop(fault.New(fault.Invalid, "Redis pub/sub payload exceeds its bound"))
		return
	}
	if err := s.buffer.Push(pubsub.Message{Channel: channel, Data: payload}); err != nil {
		s.stop(err)
	}
}

// hub multiplexes every subscription of one Client over one dedicated
// connection, with one reader and one heartbeat goroutine. It exists only while
// at least one stream is registered or reserved, then closes its connection.
// hub.mu orders registration bookkeeping with the SUBSCRIBE/UNSUBSCRIBE writes,
// so acknowledgements are matched in exactly the order commands were sent.
type hub struct {
	client   *Client
	raw      *driver.PubSub
	lifetime context.Context
	cancel   context.CancelFunc
	closing  atomic.Bool

	mu         sync.Mutex
	failure    error
	pending    int
	streams    map[*subscription]struct{}
	channels   map[string]*hubChannel
	subscribed int

	sequence                atomic.Uint64
	pong                    chan uint64
	readDone, heartbeatDone chan struct{}
	done                    chan struct{}
	closeErr                error
}

// hubChannel tracks one exact channel on the shared connection.
type hubChannel struct {
	streams map[*subscription]struct{}
	// commanded is the state requested by the last command sent; server is
	// the state confirmed by the last acknowledgement received.
	commanded, server bool
	acks              []hubAck
}

// hubAck is one outstanding acknowledgement, in send order. A SUBSCRIBE ack
// confirms the streams that were waiting for it.
type hubAck struct {
	subscribe bool
	waiters   []*subscription
}

// newHub starts a shared connection owner. Call with c.mu held and c not closing.
func newHub(c *Client) *hub {
	lifetime, cancel := context.WithCancel(context.Background())
	h := &hub{client: c, lifetime: lifetime, cancel: cancel, streams: make(map[*subscription]struct{}), channels: make(map[string]*hubChannel), pong: make(chan uint64, 1), readDone: make(chan struct{}), heartbeatDone: make(chan struct{}), done: make(chan struct{})}
	// Empty construction performs no I/O; the reader dials on first use.
	h.raw = c.raw.Subscribe(lifetime)
	c.enter()
	go h.receive()
	go h.heartbeat()
	return h
}

// register consumes one reservation. It subscribes channels not yet requested on
// the connection, joins pending acknowledgements, and is immediately ready for
// channels the connection has already confirmed.
func (h *hub) register(s *subscription) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pending--
	if h.closing.Load() {
		return errors.Join(errHubClosing, h.failure)
	}
	select {
	case <-h.client.stopping:
		h.idleLocked()
		return pubsub.ErrClosed
	default:
	}
	if err := s.buffer.Err(); err != nil {
		h.idleLocked()
		return err
	}
	h.streams[s] = struct{}{}
	var fresh []string
	for _, name := range s.names {
		channel := h.channels[name]
		if channel == nil {
			channel = &hubChannel{streams: make(map[*subscription]struct{})}
			h.channels[name] = channel
		}
		channel.streams[s] = struct{}{}
		switch last := len(channel.acks) - 1; {
		case !channel.commanded:
			channel.commanded = true
			channel.acks = append(channel.acks, hubAck{subscribe: true, waiters: []*subscription{s}})
			fresh = append(fresh, name)
			s.waiting++
		case last >= 0 && channel.acks[last].subscribe:
			channel.acks[last].waiters = append(channel.acks[last].waiters, s)
			s.waiting++
		default:
			s.confirmed[name] = true
		}
	}
	if s.waiting == 0 {
		close(s.ready)
	}
	if len(fresh) == 0 {
		return nil
	}
	// The shared connection's own bounded context writes the command, so one
	// caller's cancellation cannot interrupt a frame other streams depend on.
	write, cancel := context.WithTimeout(h.lifetime, h.client.config.OperationTimeout)
	defer cancel()
	if err := h.raw.Subscribe(write, fresh...); err != nil {
		failure := errors.Join(pubsub.ErrDisconnected, classify(write, err))
		h.failLocked(failure)
		return failure
	}
	return nil
}

// unregister removes a stream and unsubscribes channels no other stream needs.
// It returns the connection's done channel when the connection is closing (the
// stream was the last one, or the connection failed), and nil otherwise.
func (h *hub) unregister(s *subscription) <-chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, registered := h.streams[s]; !registered {
		if h.closing.Load() {
			return h.done
		}
		return nil
	}
	delete(h.streams, s)
	var gone []string
	for _, name := range s.names {
		channel := h.channels[name]
		delete(channel.streams, s)
		if len(channel.streams) == 0 && channel.commanded {
			channel.commanded = false
			channel.acks = append(channel.acks, hubAck{})
			gone = append(gone, name)
		}
	}
	if h.closing.Load() || h.idleLocked() {
		return h.done
	}
	if len(gone) > 0 {
		write, cancel := context.WithTimeout(h.lifetime, h.client.config.OperationTimeout)
		defer cancel()
		if err := h.raw.Unsubscribe(write, gone...); err != nil {
			h.failLocked(errors.Join(pubsub.ErrDisconnected, classify(write, err)))
			return h.done
		}
	}
	return nil
}

// idleLocked shuts the connection down when no stream is registered or reserved.
func (h *hub) idleLocked() bool {
	if h.closing.Load() || len(h.streams) > 0 || h.pending > 0 {
		return false
	}
	h.shutdownLocked()
	return true
}

// fail terminates every stream on the connection with the same explicit gap.
func (h *hub) fail(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failLocked(err)
}
func (h *hub) failLocked(err error) {
	if h.closing.Load() {
		return
	}
	h.failure = err
	for s := range h.streams {
		s.stop(err)
	}
	h.shutdownLocked()
}
func (h *hub) shutdownLocked() {
	h.closing.Store(true)
	h.cancel()
	go func() {
		err := h.raw.Close()
		if errors.Is(err, driver.ErrClosed) {
			err = nil
		}
		<-h.readDone
		<-h.heartbeatDone
		h.closeErr = classify(context.Background(), err)
		close(h.done)
		h.client.leave()
	}()
}

func (h *hub) receive() {
	defer close(h.readDone)
	for {
		message, err := h.raw.Receive(h.lifetime)
		if err != nil {
			if h.lifetime.Err() == nil {
				h.fail(errors.Join(pubsub.ErrDisconnected, classify(h.lifetime, err)))
			}
			return
		}
		switch message := message.(type) {
		case *driver.Message:
			err = h.deliver(message)
		case *driver.Subscription:
			err = h.acknowledge(message)
		case *driver.Pong:
			sequence := h.sequence.Load()
			if message.Payload == pingPayload(sequence) {
				select {
				case h.pong <- sequence:
				default:
				}
			} else {
				err = fault.New(fault.Invalid, "unexpected Redis pub/sub pong")
			}
		default:
			err = fault.New(fault.Invalid, "Redis pub/sub continuity was interrupted")
		}
		if err != nil {
			h.fail(errors.Join(pubsub.ErrDisconnected, err))
			return
		}
	}
}

// deliver routes one message to every stream confirmed for its exact channel.
// A message for a channel the server is still subscribed to while its
// UNSUBSCRIBE is pending has no remaining stream and is correctly unrouted.
func (h *hub) deliver(message *driver.Message) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	channel := h.channels[message.Channel]
	if message.Pattern != "" || channel == nil || !channel.server {
		return fault.New(fault.Invalid, "Redis delivered an unregistered pub/sub channel")
	}
	var payload []byte
	for s := range channel.streams {
		if !s.confirmed[message.Channel] {
			continue
		}
		if payload == nil {
			payload = []byte(message.Payload)
		}
		s.deliver(s.channels[message.Channel], payload)
	}
	return nil
}

// acknowledge matches one SUBSCRIBE/UNSUBSCRIBE reply to the command that
// caused it and checks the server's running subscription count exactly.
func (h *hub) acknowledge(message *driver.Subscription) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	invalid := fault.New(fault.Invalid, "invalid Redis subscription acknowledgement")
	channel := h.channels[message.Channel]
	if channel == nil || len(channel.acks) == 0 {
		return invalid
	}
	ack := channel.acks[0]
	channel.acks = channel.acks[1:]
	switch message.Kind {
	case "subscribe":
		if !ack.subscribe || channel.server {
			return invalid
		}
		channel.server = true
		h.subscribed++
		for _, s := range ack.waiters {
			if _, registered := channel.streams[s]; !registered || s.confirmed[message.Channel] {
				continue
			}
			s.confirmed[message.Channel] = true
			if s.waiting--; s.waiting == 0 {
				close(s.ready)
			}
		}
	case "unsubscribe":
		if ack.subscribe || !channel.server {
			return invalid
		}
		channel.server = false
		h.subscribed--
	default:
		return invalid
	}
	if message.Count != h.subscribed {
		return invalid
	}
	if !channel.server && len(channel.acks) == 0 && len(channel.streams) == 0 {
		delete(h.channels, message.Channel)
	}
	return nil
}
func pingPayload(sequence uint64) string { return "foundry-pubsub-" + strconv.FormatUint(sequence, 10) }

// A separate owned heartbeat avoids treating a timeout halfway through a RESP
// frame as an idle stream. Receive itself has no idle timeout; Close unblocks it.
func (h *hub) heartbeat() {
	defer close(h.heartbeatDone)
	interval := h.client.config.OperationTimeout
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-h.lifetime.Done():
			return
		case <-timer.C:
		}
		sequence := h.sequence.Add(1)
		ctx, cancel := context.WithTimeout(h.lifetime, interval)
		err := h.raw.Ping(ctx, pingPayload(sequence))
		cancel()
		if err != nil {
			if h.lifetime.Err() == nil {
				h.fail(errors.Join(pubsub.ErrDisconnected, classify(h.lifetime, err)))
			}
			return
		}
		timer.Reset(interval)
	waiting:
		for {
			select {
			case <-h.lifetime.Done():
				return
			case <-timer.C:
				h.fail(errors.Join(pubsub.ErrDisconnected, fault.New(fault.Timeout, "Redis pub/sub heartbeat timed out")))
				return
			case acknowledged := <-h.pong:
				if acknowledged != sequence {
					continue
				}
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				break waiting
			}
		}
		timer.Reset(interval)
	}
}
