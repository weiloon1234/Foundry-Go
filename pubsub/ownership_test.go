package pubsub_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/pubsub"
)

func TestKeyCallbackRetainsOperationUntilExit(t *testing.T) {
	broker, _, _ := fixture(t, func(c *pubsub.Config) { c.MaxConcurrent = 1 })
	entered, release := make(chan struct{}), make(chan struct{})
	topic, err := pubsub.Define[memberID, payload]("held-keys", 1, keyspace.NewCodec(func(memberID) (string, error) { close(entered); <-release; return "key", nil })).Bind(broker)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := topic.Publish(t.Context(), 1, payload{Text: "held", Labels: []string{}})
		result <- err
	}()
	<-entered
	// A full broker queues briefly, then reports retryable overload.
	bounded, stop := context.WithTimeout(t.Context(), 20*time.Millisecond)
	if _, err := topic.Publish(bounded, 2, payload{}); !errors.Is(err, fault.Overloaded) {
		t.Error(err)
	}
	stop()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := broker.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Error(err)
	}
	select {
	case <-broker.Done():
		t.Error("broker abandoned key callback")
	default:
	}
	if broker.Stats().Operations != 1 {
		t.Error(broker.Stats())
	}
	close(release)
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := broker.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

type decoderGate struct {
	entered, release chan struct{}
	once             sync.Once
}

var activeDecoder atomic.Pointer[decoderGate]

type heldPayload struct{ Value string }

func (p *heldPayload) UnmarshalJSON(data []byte) error {
	if gate := activeDecoder.Load(); gate != nil {
		gate.once.Do(func() { close(gate.entered) })
		<-gate.release
	}
	type wire heldPayload
	return json.Unmarshal(data, (*wire)(p))
}
func TestDecoderRetainsSubscriptionUntilExit(t *testing.T) {
	broker, _, backend := fixture(t, nil)
	topic, err := pubsub.Define[memberID, heldPayload]("held-decoder", 1, keyspace.UnsignedKeys[memberID]()).Bind(broker)
	if err != nil {
		t.Fatal(err)
	}
	s, err := topic.Subscribe(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	channel, _ := pubsub.NewChannel(broker.Namespace(), "held-decoder", 1, "1")
	if _, err := backend.Publish(t.Context(), channel, []byte(`{"Value":"ready"}`)); err != nil {
		t.Fatal(err)
	}
	gate := &decoderGate{entered: make(chan struct{}), release: make(chan struct{})}
	activeDecoder.Store(gate)
	defer activeDecoder.Store(nil)
	result := make(chan error, 1)
	go func() { _, err := s.Receive(t.Context()); result <- err }()
	<-gate.entered
	if _, err := s.Receive(t.Context()); !errors.Is(err, fault.Conflict) {
		t.Error(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := broker.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Error(err)
	}
	select {
	case <-s.Done():
		t.Error("subscription abandoned decoder")
	default:
	}
	select {
	case <-broker.Done():
		t.Error("broker abandoned decoder")
	default:
	}
	close(gate.release)
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := broker.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if broker.Stats().Subscriptions != 0 {
		t.Fatal(broker.Stats())
	}
}
func TestMalformedTypedPayloadTerminatesWithoutSkipping(t *testing.T) {
	broker, topic, backend := fixture(t, nil)
	s, err := topic.Subscribe(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	channel, _ := pubsub.NewChannel(broker.Namespace(), changed.Name(), changed.Version(), "1")
	if _, err := backend.Publish(t.Context(), channel, []byte(`{"unexpected":true}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Receive(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	waitDone(t, s.Done())
	if !errors.Is(s.Err(), fault.Invalid) {
		t.Fatal(s.Err())
	}
}
