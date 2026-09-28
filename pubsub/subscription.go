package pubsub

import (
	"context"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/value"
)

type subscriptionState struct {
	broker               *Broker
	channel              Channel
	stream               Stream
	ready, done          chan struct{}
	rawDone              <-chan struct{}
	watchDone            chan struct{}
	lifetime             context.Context
	cancel               context.CancelFunc
	mu                   sync.Mutex
	closing, reading     bool
	terminal, cleanupErr error
	drained              chan struct{}
}

func (b *Broker) reserve() (*subscriptionState, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closing {
		return nil, ErrClosed
	}
	if len(b.subscriptions) >= b.config.MaxSubscriptions {
		return nil, fault.New(fault.Conflict, "pub/sub subscription capacity reached")
	}
	lifetime, cancel := context.WithCancel(b.lifetime)
	s := &subscriptionState{broker: b, ready: make(chan struct{}), done: make(chan struct{}), drained: make(chan struct{}), watchDone: make(chan struct{}), lifetime: lifetime, cancel: cancel}
	b.subscriptions[s] = struct{}{}
	go s.watch()
	return s, nil
}
func (s *subscriptionState) beginClose(reason error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return
	}
	s.closing = true
	s.terminal = reason
	s.cancel()
	if !s.reading {
		close(s.drained)
	}
	go func() {
		<-s.ready
		var err error
		if s.stream != nil {
			err = callback.Isolated("pub/sub subscription cleanup", func() error { return s.stream.Close(context.Background()) })
			if s.rawDone != nil {
				<-s.rawDone
			}
		}
		<-s.watchDone
		<-s.drained
		s.mu.Lock()
		s.cleanupErr = err
		s.mu.Unlock()
		s.broker.remove(s, err)
	}()
}
func (s *subscriptionState) acquire(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return s.terminal
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.reading {
		return fault.New(fault.Conflict, "pub/sub subscription already has an active receiver")
	}
	s.reading = true
	return nil
}
func (s *subscriptionState) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reading = false
	if s.closing {
		close(s.drained)
	}
}

// Subscription owns one typed stream. Receive is sequential, including JSON
// decoding; concurrent receives fail instead of creating unbounded waiting work.
// Terminal stream/decoding failures discard pending delivery and close the stream.
type Subscription[V any] struct{ state *subscriptionState }

func (s *Subscription[V]) Done() <-chan struct{} {
	if s == nil || s.state == nil {
		return nil
	}
	return s.state.done
}
func (s *Subscription[V]) Err() error {
	if s == nil || s.state == nil {
		return fault.New(fault.Invalid, "pub/sub subscription is not initialized")
	}
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	return s.state.terminal
}
func (s *Subscription[V]) Close(ctx context.Context) error {
	if s == nil || s.state == nil || ctx == nil {
		return fault.New(fault.Invalid, "pub/sub close needs subscription and context")
	}
	for frame, _ := ctx.Value(operationKey{}).(*operationFrame); frame != nil; frame = frame.parent {
		if frame.subscription == s.state && frame.active.Load() {
			return fault.New(fault.Cycle, "pub/sub receiver cannot wait for its own shutdown")
		}
	}
	s.state.beginClose(ErrClosed)
	select {
	case <-s.state.done:
		return s.state.cleanupErr
	default:
	}
	select {
	case <-s.state.done:
		return s.state.cleanupErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Subscription[V]) Receive(ctx context.Context) (V, error) {
	if s == nil || s.state == nil || ctx == nil {
		return *new(V), fault.New(fault.Invalid, "pub/sub receive needs subscription and context")
	}
	state := s.state
	if err := state.acquire(ctx); err != nil {
		return *new(V), err
	}
	defer state.release()
	operation, release := state.broker.operation(ctx, state)
	defer release()

	var output V
	err := callback.Isolated("pub/sub receive", func() error {
		if err := operation.Err(); err != nil {
			return err
		}
		message, err := state.stream.Next(operation)
		if err != nil {
			return err
		}
		if message.Channel != state.channel || len(message.Data) > state.broker.config.Buffer.PayloadBytes {
			return fault.New(fault.Invalid, "pub/sub adapter returned an invalid channel or oversized payload")
		}
		snapshot, err := value.ParseJSON[V](string(message.Data))
		if err != nil {
			return err
		}
		output, err = snapshot.Decode()
		return err
	})
	if err == nil {
		err = operation.Err()
	}
	if err != nil {
		// A canceled receive wait is not an instruction to discard the subscription.
		canceledWait := false
		if ctx.Err() != nil {
			failed := callback.Isolated("classify pub/sub receive", func() error {
				canceledWait = errorgraph.Is(err, ctx.Err())
				return nil
			})
			if failed != nil {
				err = failed
				canceledWait = false
			}
		}
		if !canceledWait {
			state.beginClose(err)
		}
		return *new(V), err
	}
	return output, nil
}

// Each registered subscription owns one watcher, including during setup. A raw
// adapter termination is visible even when the application is not receiving.
func (s *subscriptionState) watch() {
	defer close(s.watchDone)
	<-s.ready
	select {
	case <-s.lifetime.Done():
		return
	case <-s.rawDone:
		reason := callback.Isolated("pub/sub terminal status", func() error { return s.stream.Err() })
		if reason == nil {
			reason = ErrDisconnected
		}
		s.beginClose(reason)
	}
}
