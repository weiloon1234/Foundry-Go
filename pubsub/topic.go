package pubsub

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Topic retains concrete keys and payloads through publication and subscription.
type Topic[K, V any] struct {
	broker     *Broker
	definition *definition[K, V]
}

func (t Topic[K, V]) Name() Name       { return (Declaration[K, V]{t.definition}).Name() }
func (t Topic[K, V]) Version() Version { return (Declaration[K, V]{t.definition}).Version() }
func (t Topic[K, V]) Validate() error {
	if t.broker == nil || t.broker.done == nil {
		return fault.New(fault.Invalid, "pub/sub topic is not bound")
	}
	return (Declaration[K, V]{t.definition}).Validate()
}
func (t Topic[K, V]) address(ctx context.Context, key K) (Channel, error) {
	logical, err := t.definition.keys.Encode(key)
	if err != nil {
		return Channel{}, err
	}
	if err := ctx.Err(); err != nil {
		return Channel{}, err
	}
	if len(logical) > t.broker.config.MaxKeyBytes {
		return Channel{}, fault.New(fault.Invalid, "pub/sub logical key exceeds its bound")
	}
	return NewChannel(t.broker.config.Namespace, t.Name(), t.Version(), logical)
}

// Publish snapshots typed JSON using Foundry's shared value validation. The return
// count describes subscriptions at the authority, not completed application work.
// A lost acknowledgement is an error even if publication already happened.
func (t Topic[K, V]) Publish(ctx context.Context, key K, input V) (uint64, error) {
	if err := t.Validate(); err != nil {
		return 0, err
	}
	var count uint64
	err := t.broker.execute(ctx, func(ctx context.Context) error {
		channel, err := t.address(ctx, key)
		if err != nil {
			return err
		}
		snapshot, err := value.NewJSON(input)
		if err != nil {
			return err
		}
		text, err := snapshot.Text()
		if err != nil {
			return err
		}
		if len(text) > t.broker.config.Buffer.PayloadBytes {
			return fault.New(fault.Invalid, "pub/sub encoded payload exceeds its bound")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		count, err = t.broker.backend.Publish(ctx, channel, []byte(text))
		return err
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// Subscribe waits for confirmed readiness. Its context bounds establishment only;
// the Broker owns the subscription afterwards. Close it when no longer needed.
// Receive waits use their own contexts and do not implicitly resubscribe.
func (t Topic[K, V]) Subscribe(ctx context.Context, key K) (*Subscription[V], error) {
	state, err := t.subscribe(ctx, []K{key})
	if err != nil {
		return nil, err
	}
	return &Subscription[V]{state: state}, nil
}

// SubscribeKeys confirms readiness for every key in one subscription, whose
// Receive reports which typed key each payload was published under. Duplicate
// keys are rejected; the count is bounded by Config.Buffer.Channels. Keys are
// retained for delivery and must stay immutable while the subscription lives.
func (t Topic[K, V]) SubscribeKeys(ctx context.Context, first K, rest ...K) (*KeyedSubscription[K, V], error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	if len(rest) >= t.broker.config.Buffer.Channels {
		return nil, fault.New(fault.Invalid, "pub/sub channel count exceeds its bound")
	}
	keys := append([]K{first}, rest...)
	state, err := t.subscribe(ctx, keys)
	if err != nil {
		return nil, err
	}
	return &KeyedSubscription[K, V]{typed: Subscription[V]{state: state}, keys: keys}, nil
}

func (t Topic[K, V]) subscribe(ctx context.Context, keys []K) (*subscriptionState, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	var state *subscriptionState
	err := t.broker.execute(ctx, func(ctx context.Context) error {
		var err error
		state, err = t.broker.reserve()
		if err != nil {
			return err
		}
		defer close(state.ready)
		channels := make([]Channel, len(keys))
		state.channels = make(map[Channel]int, len(keys))
		for i, key := range keys {
			channel, err := t.address(ctx, key)
			if err != nil {
				return err
			}
			if _, duplicate := state.channels[channel]; duplicate {
				return fault.New(fault.Duplicate, "pub/sub subscription repeats a key")
			}
			channels[i] = channel
			state.channels[channel] = i
		}
		state.stream, err = t.broker.backend.Subscribe(ctx, channels, t.broker.config.Buffer)
		if state.stream != nil {
			state.rawDone = state.stream.Done()
		}
		if err != nil {
			return err
		}
		if state.stream == nil {
			return fault.New(fault.Internal, "pub/sub adapter returned an empty stream")
		}
		if state.rawDone == nil {
			return fault.New(fault.Internal, "pub/sub adapter returned an unowned stream")
		}
		return nil
	})
	if err != nil {
		if state != nil {
			state.beginClose(err)
			<-state.done
			err = errors.Join(err, state.cleanupErr)
		}
		return nil, err
	}
	select {
	case <-t.broker.lifetime.Done():
		state.beginClose(ErrClosed)
		<-state.done
		return nil, errors.Join(ErrClosed, state.cleanupErr)
	default:
	}
	return state, nil
}

// Delivery pairs a received payload with the typed key it was published under.
type Delivery[K, V any] struct {
	Key   K
	Value V
}

// KeyedSubscription is one typed stream over several keys of the same topic. It
// shares Subscription's ownership, sequential receive and failure semantics.
type KeyedSubscription[K, V any] struct {
	typed Subscription[V]
	keys  []K
}

func (s *KeyedSubscription[K, V]) Done() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.typed.Done()
}
func (s *KeyedSubscription[K, V]) Err() error {
	if s == nil {
		return fault.New(fault.Invalid, "pub/sub subscription is not initialized")
	}
	return s.typed.Err()
}
func (s *KeyedSubscription[K, V]) Close(ctx context.Context) error {
	if s == nil {
		return fault.New(fault.Invalid, "pub/sub close needs subscription and context")
	}
	return s.typed.Close(ctx)
}

// Receive returns the next payload together with the key it was published to.
func (s *KeyedSubscription[K, V]) Receive(ctx context.Context) (Delivery[K, V], error) {
	if s == nil || s.typed.state == nil || ctx == nil {
		return Delivery[K, V]{}, fault.New(fault.Invalid, "pub/sub receive needs subscription and context")
	}
	position, value, err := receive[V](ctx, s.typed.state)
	if err != nil {
		return Delivery[K, V]{}, err
	}
	return Delivery[K, V]{Key: s.keys[position], Value: value}, nil
}
