package pubsub_test

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/pubsub"
	"github.com/weiloon1234/Foundry-Go/pubsub/memory"
)

type memberID uint64
type payload struct {
	Text   string
	Labels []string
}

var changed = pubsub.Define[memberID, payload]("member-changed", 1, keyspace.UnsignedKeys[memberID]())

func fixture(t *testing.T, configure func(*pubsub.Config)) (*pubsub.Broker, pubsub.Topic[memberID, payload], *memory.Backend) {
	t.Helper()
	backend, err := memory.New(20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backend.Close() })
	config := pubsub.DefaultConfig(keyspace.Namespace{Application: "test", Environment: "typed-pubsub"})
	if configure != nil {
		configure(&config)
	}
	broker, err := pubsub.NewBroker(backend, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := broker.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	topic, err := changed.Bind(broker)
	if err != nil {
		t.Fatal(err)
	}
	return broker, topic, backend
}
func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("owned subscription did not close")
	}
}
func receive(t *testing.T, s *pubsub.Subscription[payload]) payload {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	v, err := s.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestTypedFanoutReadinessAndIndependentSnapshots(t *testing.T) {
	broker, topic, _ := fixture(t, nil)
	a, err := topic.Subscribe(t.Context(), 7)
	if err != nil {
		t.Fatal(err)
	}
	b, err := topic.Subscribe(t.Context(), 7)
	if err != nil {
		t.Fatal(err)
	}
	input := payload{Text: "ready", Labels: []string{"original"}}
	if count, err := topic.Publish(t.Context(), 7, input); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	input.Labels[0] = "mutated"
	first := receive(t, a)
	first.Labels[0] = "first"
	second := receive(t, b)
	if second.Text != "ready" || second.Labels[0] != "original" {
		t.Fatal(second)
	}
	if count, err := topic.Publish(t.Context(), 8, input); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if err := a.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitDone(t, a.Done())
	if !errors.Is(a.Err(), pubsub.ErrClosed) {
		t.Fatal(a.Err())
	}
	if err := broker.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitDone(t, b.Done())
	if _, err := topic.Publish(t.Context(), 7, input); !errors.Is(err, pubsub.ErrClosed) {
		t.Fatal(err)
	}
}
func TestReceiveCancellationKeepsSubscriptionAndBrokerOwnsClose(t *testing.T) {
	broker, topic, _ := fixture(t, nil)
	ctx, cancel := context.WithCancel(t.Context())
	s, err := topic.Subscribe(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := s.Receive(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if s.Err() != nil {
		t.Fatal(s.Err())
	}
	if _, err := topic.Publish(t.Context(), 1, payload{Text: "later", Labels: []string{}}); err != nil {
		t.Fatal(err)
	}
	if got := receive(t, s); got.Text != "later" {
		t.Fatal(got)
	}
	finished := make(chan error, 1)
	go func() { _, err := s.Receive(context.Background()); finished <- err }()
	if err := broker.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err == nil {
		t.Fatal("closed receiver succeeded")
	}
}
func TestOverflowIsVisibleWithoutAnActiveReceiver(t *testing.T) {
	_, topic, _ := fixture(t, func(c *pubsub.Config) { c.Buffer.Messages = 1 })
	s, err := topic.Subscribe(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := topic.Publish(t.Context(), 1, payload{Text: "overflow", Labels: []string{}}); err != nil {
			t.Fatal(err)
		}
	}
	waitDone(t, s.Done())
	if !errors.Is(s.Err(), pubsub.ErrOverflow) {
		t.Fatal(s.Err())
	}
	if _, err := s.Receive(t.Context()); !errors.Is(err, pubsub.ErrOverflow) {
		t.Fatal(err)
	}
}
func TestNamespaceVersionAndDeclarationOwnership(t *testing.T) {
	broker, topic, backend := fixture(t, nil)
	if _, err := pubsub.Define[memberID, payload]("member-changed", 1, keyspace.UnsignedKeys[memberID]()).Bind(broker); !errors.Is(err, fault.Duplicate) {
		t.Fatal(err)
	}
	if _, err := changed.Bind(broker); err != nil {
		t.Fatal(err)
	}
	other, err := pubsub.NewBroker(backend, pubsub.DefaultConfig(keyspace.Namespace{Application: "other", Environment: "typed-pubsub"}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close(context.Background()) })
	isolated, err := changed.Bind(other)
	if err != nil {
		t.Fatal(err)
	}
	s, err := isolated.Subscribe(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	newer, err := pubsub.Define[memberID, payload]("member-changed", 2, keyspace.UnsignedKeys[memberID]()).Bind(broker)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := newer.Subscribe(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := topic.Publish(t.Context(), 1, payload{Text: "none", Labels: []string{}}); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if s.Err() != nil || v2.Err() != nil {
		t.Fatal("isolated subscriber failed")
	}
}
func TestSubscriptionCapacityAndConcurrentShutdown(t *testing.T) {
	broker, topic, _ := fixture(t, func(c *pubsub.Config) { c.MaxSubscriptions = 1 })
	s, err := topic.Subscribe(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := topic.Subscribe(t.Context(), 2); !errors.Is(err, fault.Conflict) {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if err := s.Close(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if broker.Stats().Subscriptions != 0 {
		t.Fatal("closed subscription retained broker capacity")
	}
	replacement, err := topic.Subscribe(t.Context(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := replacement.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Broker shutdown waits for removal as well as stream exit.
	if err := broker.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := changed.Bind(broker); !errors.Is(err, pubsub.ErrClosed) {
		t.Fatal(err)
	}
}

type badPayload struct{ Mode string }

func (p badPayload) MarshalJSON() ([]byte, error) {
	switch p.Mode {
	case "panic":
		panic("private")
	case "goexit":
		runtime.Goexit()
	}
	return []byte("invalid"), nil
}
func TestPayloadCallbacksCannotEscape(t *testing.T) {
	broker, _, _ := fixture(t, nil)
	topic, err := pubsub.Define[memberID, badPayload]("bad", 1, keyspace.UnsignedKeys[memberID]()).Bind(broker)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"panic", "goexit", "malformed"} {
		if count, err := topic.Publish(t.Context(), 1, badPayload{mode}); err == nil || count != 0 || strings.Contains(err.Error(), "private") {
			t.Fatal(count, err)
		}
	}
}
func TestAdapterCloseIsObservedWithoutReceive(t *testing.T) {
	_, topic, backend := fixture(t, nil)
	s, err := topic.Subscribe(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	backend.Close()
	waitDone(t, s.Done())
	if !errors.Is(s.Err(), pubsub.ErrClosed) {
		t.Fatal(s.Err())
	}
}
