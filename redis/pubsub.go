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
	"github.com/weiloon1234/Foundry-Go/internal/contextlink"
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

// Subscribe owns a dedicated connection and confirms every exact channel before
// returning. Establishment context cancellation never leaves an unowned reader.
// Later failure terminates the stream explicitly; vendor reconnect attempts do
// not imply continuous delivery or cause automatic public resubscription.
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
	defer c.release()
	attempt, release := contextlink.Link(ctx, sub.lifetime)
	defer release()
	attempt, cancel := context.WithTimeout(attempt, c.config.OperationTimeout)
	defer cancel()
	// Close the dedicated socket on cancellation, including while awaiting the
	// first acknowledgement. The subscriber cleanup still waits for actual exit.
	canceled := make(chan struct{})
	stopCancel := context.AfterFunc(attempt, func() { defer close(canceled); sub.stop(attempt.Err()) })
	finishCancel := func() {
		if !stopCancel() {
			<-canceled
		}
	}
	err = sub.confirm(attempt)
	finishCancel()
	if err == nil {
		err = attempt.Err()
	}
	if err != nil {
		sub.stop(classify(attempt, err))
		close(sub.readDone)
		close(sub.heartbeatDone)
		<-sub.done
		return nil, errors.Join(classify(attempt, err), sub.cleanupErr)
	}
	go sub.receive()
	go sub.heartbeat()
	return sub, nil
}

func (c *Client) reserveSubscription(ctx context.Context, channels []pubsub.Channel, limits pubsub.Limits) (*subscription, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing {
		return nil, pubsub.ErrClosed
	}
	if !c.ready {
		return nil, fault.New(fault.Conflict, "Redis client has not started successfully")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.active >= c.config.MaxOperations || len(c.subscriptions) >= c.config.MaxSubscriptions {
		return nil, fault.New(fault.Conflict, "Redis subscription capacity reached")
	}
	buffer, err := pubsubstream.New(limits)
	if err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithCancel(context.Background())
	// Empty construction performs no I/O and does not discard a Subscribe error.
	sub := &subscription{client: c, raw: c.raw.Subscribe(lifetime), buffer: buffer, lifetime: lifetime, cancel: cancel, channels: make(map[string]pubsub.Channel, len(channels)), names: make([]string, len(channels)), done: make(chan struct{}), readDone: make(chan struct{}), heartbeatDone: make(chan struct{}), pong: make(chan uint64, 1), payloadLimit: limits.PayloadBytes}
	for i, channel := range channels {
		sub.names[i] = channel.String()
		sub.channels[sub.names[i]] = channel
	}
	c.active++
	c.subscriptions[sub] = struct{}{}
	return sub, nil
}

type subscription struct {
	client                        *Client
	raw                           *driver.PubSub
	buffer                        *pubsubstream.Buffer
	lifetime                      context.Context
	cancel                        context.CancelFunc
	channels                      map[string]pubsub.Channel
	names                         []string
	once                          sync.Once
	done, readDone, heartbeatDone chan struct{}
	cleanupErr                    error
	payloadLimit                  int
	sequence                      atomic.Uint64
	pong                          chan uint64
}

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
func (s *subscription) stop(reason error) {
	s.once.Do(func() {
		s.cancel()
		s.buffer.Finish(reason)
		go func() {
			err := s.raw.Close()
			if errors.Is(err, driver.ErrClosed) {
				err = nil
			}
			<-s.readDone
			<-s.heartbeatDone
			s.cleanupErr = classify(context.Background(), err)
			s.client.mu.Lock()
			delete(s.client.subscriptions, s)
			close(s.done)
			if s.client.closing && s.client.active == 0 && len(s.client.subscriptions) == 0 {
				close(s.client.drained)
			}
			s.client.mu.Unlock()
		}()
	})
}
func (s *subscription) confirm(ctx context.Context) error {
	if err := s.raw.Subscribe(ctx, s.names...); err != nil {
		return err
	}
	acknowledged := make(map[string]bool, len(s.names))
	for len(acknowledged) < len(s.names) {
		message, err := s.raw.Receive(ctx)
		if err != nil {
			return err
		}
		switch message := message.(type) {
		case *driver.Subscription:
			if message.Kind != "subscribe" || acknowledged[message.Channel] || s.channels[message.Channel] == (pubsub.Channel{}) || message.Count != len(acknowledged)+1 {
				return fault.New(fault.Invalid, "invalid Redis subscription acknowledgement")
			}
			acknowledged[message.Channel] = true
		case *driver.Message:
			if !acknowledged[message.Channel] {
				return fault.New(fault.Invalid, "Redis delivered before channel acknowledgement")
			}
			if err := s.deliver(message); err != nil {
				return err
			}
		default:
			return fault.New(fault.Invalid, "unexpected Redis subscription setup frame")
		}
	}
	return ctx.Err()
}
func (s *subscription) deliver(message *driver.Message) error {
	if len(message.Payload) > s.payloadLimit {
		return fault.New(fault.Invalid, "Redis pub/sub payload exceeds its bound")
	}
	channel, ok := s.channels[message.Channel]
	if !ok || message.Pattern != "" {
		return fault.New(fault.Invalid, "Redis delivered an unregistered pub/sub channel")
	}
	return s.buffer.Push(pubsub.Message{Channel: channel, Data: []byte(message.Payload)})
}
func (s *subscription) receive() {
	defer close(s.readDone)
	for {
		message, err := s.raw.Receive(s.lifetime)
		if err != nil {
			if s.lifetime.Err() == nil {
				s.stop(errors.Join(pubsub.ErrDisconnected, classify(s.lifetime, err)))
			}
			return
		}
		switch message := message.(type) {
		case *driver.Message:
			err = s.deliver(message)
		case *driver.Pong:
			sequence := s.sequence.Load()
			if message.Payload == pingPayload(sequence) {
				select {
				case s.pong <- sequence:
				default:
				}
			} else {
				err = fault.New(fault.Invalid, "unexpected Redis pub/sub pong")
			}
		default:
			err = fault.New(fault.Invalid, "Redis pub/sub continuity was interrupted")
		}
		if err != nil {
			s.stop(err)
			return
		}
	}
}
func pingPayload(sequence uint64) string { return "foundry-pubsub-" + strconv.FormatUint(sequence, 10) }

// A separate owned heartbeat avoids treating a timeout halfway through a RESP
// frame as an idle stream. Receive itself has no idle timeout; Close unblocks it.
func (s *subscription) heartbeat() {
	defer close(s.heartbeatDone)
	timer := time.NewTimer(s.client.config.OperationTimeout)
	defer timer.Stop()
	for {
		select {
		case <-s.lifetime.Done():
			return
		case <-timer.C:
		}
		sequence := s.sequence.Add(1)
		ctx, cancel := context.WithTimeout(s.lifetime, s.client.config.OperationTimeout)
		err := s.raw.Ping(ctx, pingPayload(sequence))
		cancel()
		if err != nil {
			if s.lifetime.Err() == nil {
				s.stop(errors.Join(pubsub.ErrDisconnected, classify(s.lifetime, err)))
			}
			return
		}
		timer.Reset(s.client.config.OperationTimeout)
	waiting:
		for {
			select {
			case <-s.lifetime.Done():
				return
			case <-timer.C:
				s.stop(errors.Join(pubsub.ErrDisconnected, fault.New(fault.Timeout, "Redis pub/sub heartbeat timed out")))
				return
			case acknowledged := <-s.pong:
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
		timer.Reset(s.client.config.OperationTimeout)
	}
}
