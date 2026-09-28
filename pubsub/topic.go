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
		channel, err := t.address(ctx, key)
		if err != nil {
			return err
		}
		state.channel = channel
		state.stream, err = t.broker.backend.Subscribe(ctx, []Channel{channel}, t.broker.config.Buffer)
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
	return &Subscription[V]{state: state}, nil
}
