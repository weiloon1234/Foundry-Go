package pubsub_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/pubsub"
)

func TestBrokerAdmissionQueuesAndCloseStopsWaiters(t *testing.T) {
	broker, _, _ := fixture(t, func(c *pubsub.Config) { c.MaxConcurrent = 1 })
	entered, release := make(chan struct{}), make(chan struct{})
	held := true
	topic, err := pubsub.Define[memberID, payload]("queued-keys", 1, keyspace.NewCodec(func(memberID) (string, error) {
		if held {
			held = false
			close(entered)
			<-release
		}
		return "key", nil
	})).Bind(broker)
	if err != nil {
		t.Fatal(err)
	}
	first := make(chan error, 1)
	go func() {
		_, err := topic.Publish(t.Context(), 1, payload{Text: "first", Labels: []string{}})
		first <- err
	}()
	<-entered
	// A queued caller proceeds once capacity frees instead of failing.
	second := make(chan error, 1)
	go func() {
		_, err := topic.Publish(t.Context(), 2, payload{Text: "second", Labels: []string{}})
		second <- err
	}()
	time.Sleep(10 * time.Millisecond)
	close(release)
	for _, result := range []chan error{first, second} {
		if err := <-result; err != nil {
			t.Fatal(err)
		}
	}
	if stats := broker.Stats(); stats.Operations != 0 {
		t.Fatal(stats)
	}
	// Shutdown ends a queued wait with ErrClosed.
	blocked, unblock := make(chan struct{}), make(chan struct{})
	stuck, err := pubsub.Define[memberID, payload]("stuck-keys", 1, keyspace.NewCodec(func(memberID) (string, error) {
		close(blocked)
		<-unblock
		return "key", nil
	})).Bind(broker)
	if err != nil {
		t.Fatal(err)
	}
	holder := make(chan error, 1)
	go func() { _, err := stuck.Publish(t.Context(), 1, payload{Labels: []string{}}); holder <- err }()
	<-blocked
	waiter := make(chan error, 1)
	go func() { _, err := topic.Publish(t.Context(), 3, payload{Labels: []string{}}); waiter <- err }()
	time.Sleep(10 * time.Millisecond)
	closing, cancel := context.WithCancel(t.Context())
	cancel()
	_ = broker.Close(closing)
	if err := <-waiter; !errors.Is(err, pubsub.ErrClosed) {
		t.Fatal("queued publication survived shutdown", err)
	}
	close(unblock)
	if err := <-holder; err == nil {
		t.Fatal("canceled holder published")
	}
}

func TestSubscribeKeysReportsTypedKeys(t *testing.T) {
	_, topic, _ := fixture(t, nil)
	s, err := topic.SubscribeKeys(t.Context(), 7, 8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(context.Background()) })
	for _, key := range []memberID{8, 7} {
		if count, err := topic.Publish(t.Context(), key, payload{Text: "for", Labels: []string{}}); err != nil || count != 1 {
			t.Fatal(count, err)
		}
	}
	for _, want := range []memberID{8, 7} {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		got, err := s.Receive(ctx)
		cancel()
		if err != nil || got.Key != want || got.Value.Text != "for" {
			t.Fatal(got, err)
		}
	}
	if _, err := topic.SubscribeKeys(t.Context(), 1, 1); !errors.Is(err, fault.Duplicate) {
		t.Fatal(err)
	}
	many := make([]memberID, pubsub.DefaultLimits().Channels)
	for i := range many {
		many[i] = memberID(i + 100)
	}
	if _, err := topic.SubscribeKeys(t.Context(), 1, many...); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	// A zero Topic is rejected before its (missing) broker configuration is read.
	var unbound pubsub.Topic[memberID, payload]
	if _, err := unbound.SubscribeKeys(t.Context(), 1, 2); !errors.Is(err, fault.Invalid) {
		t.Fatal("zero topic was not rejected", err)
	}
	if err := s.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(s.Err(), pubsub.ErrClosed) {
		t.Fatal(s.Err())
	}
}

func TestSuperviseReportsGapBeforeResumedDelivery(t *testing.T) {
	broker, topic, _ := fixture(t, func(c *pubsub.Config) { c.Buffer.Messages = 1 })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	events := make(chan string, 8)
	entered, release := make(chan struct{}), make(chan struct{})
	gaps := make(chan pubsub.Gap, 1)
	policy := pubsub.SupervisePolicy{InitialBackoff: time.Millisecond, MaxBackoff: 2 * time.Millisecond}
	result := make(chan error, 1)
	go func() {
		result <- topic.Supervise(ctx, 1, policy, func(_ context.Context, value payload) error {
			if value.Text == "hold" {
				close(entered)
				<-release
			}
			events <- value.Text
			return nil
		}, func(_ context.Context, gap pubsub.Gap) error {
			events <- "gap"
			gaps <- gap
			return nil
		})
	}()
	publish := func(text string) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for {
			count, err := topic.Publish(t.Context(), 1, payload{Text: text, Labels: []string{}})
			if err != nil {
				t.Fatal(err)
			}
			if count == 1 || time.Now().After(deadline) {
				return
			}
			time.Sleep(time.Millisecond)
		}
	}
	publish("hold")
	<-entered
	// One queued payload fits; the next overflows and ends the subscription.
	publish("queued")
	publish("lost")
	close(release)
	gap := <-gaps
	if !errors.Is(gap.Cause, pubsub.ErrOverflow) {
		t.Fatal(gap)
	}
	publish("after")
	for _, want := range []string{"hold", "gap", "after"} {
		select {
		case got := <-events:
			if got != want {
				t.Fatal("unexpected event order", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("missing supervised event", want)
		}
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	waitSubscriptions(t, broker, 0)
}

func TestSuperviseEndsOnHandlerFailureInvalidKeysAndShutdown(t *testing.T) {
	broker, topic, _ := fixture(t, nil)
	policy := pubsub.DefaultSupervisePolicy()
	ignore := func(context.Context, pubsub.Gap) error { return nil }
	cause := errors.New("handler failed")
	result := make(chan error, 1)
	go func() {
		result <- topic.Supervise(t.Context(), 1, policy, func(context.Context, payload) error { return cause }, ignore)
	}()
	for {
		if count, err := topic.Publish(t.Context(), 1, payload{Labels: []string{}}); err != nil {
			t.Fatal(err)
		} else if count == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err := <-result; !errors.Is(err, cause) {
		t.Fatal(err)
	}
	waitSubscriptions(t, broker, 0)
	invalid, err := pubsub.Define[memberID, payload]("invalid-keys", 1, keyspace.NewCodec(func(memberID) (string, error) { return "", nil })).Bind(broker)
	if err != nil {
		t.Fatal(err)
	}
	if err := invalid.Supervise(t.Context(), 1, policy, func(context.Context, payload) error { return nil }, ignore); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if err := topic.Supervise(t.Context(), 1, pubsub.SupervisePolicy{}, func(context.Context, payload) error { return nil }, ignore); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if err := topic.Supervise(t.Context(), 1, policy, func(context.Context, payload) error { return nil }, nil); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	go func() {
		result <- topic.Supervise(t.Context(), 1, policy, func(context.Context, payload) error { return nil }, ignore)
	}()
	waitSubscriptions(t, broker, 1)
	if err := broker.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, pubsub.ErrClosed) {
		t.Fatal(err)
	}
}

func waitSubscriptions(t *testing.T, broker *pubsub.Broker, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for broker.Stats().Subscriptions != want {
		if time.Now().After(deadline) {
			t.Fatal("subscriptions did not settle", broker.Stats(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

// failingSubscriptions fails the first establishment attempts with the given
// adapter errors, then delegates to a working backend.
type failingSubscriptions struct {
	pubsub.Backend
	failures []error
	calls    int
}

func (b *failingSubscriptions) Subscribe(ctx context.Context, channels []pubsub.Channel, limits pubsub.Limits) (pubsub.Stream, error) {
	b.calls++
	if b.calls <= len(b.failures) {
		return nil, b.failures[b.calls-1]
	}
	return b.Backend.Subscribe(ctx, channels, limits)
}

func TestSuperviseRetriesDisconnectsAndStopsOnlyOnShutdownSentinel(t *testing.T) {
	_, _, backend := fixture(t, nil)
	supervise := func(failures ...error) (*failingSubscriptions, []pubsub.Gap, error) {
		flaky := &failingSubscriptions{Backend: backend, failures: failures}
		broker, err := pubsub.NewBroker(flaky, pubsub.DefaultConfig(keyspace.Namespace{Application: "test", Environment: "typed-pubsub"}))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { broker.Close(context.Background()) })
		topic, err := changed.Bind(broker)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		var gaps []pubsub.Gap
		policy := pubsub.SupervisePolicy{InitialBackoff: time.Millisecond, MaxBackoff: 2 * time.Millisecond}
		err = topic.Supervise(ctx, 1, policy, func(context.Context, payload) error { return nil }, func(_ context.Context, gap pubsub.Gap) error {
			gaps = append(gaps, gap)
			cancel()
			return nil
		})
		return flaky, gaps, err
	}
	// A protocol anomaly while establishing is a disconnect, not bad input, and
	// a connection closing underneath an attempt is not the broker's shutdown.
	flaky, gaps, err := supervise(
		errors.Join(pubsub.ErrDisconnected, fault.New(fault.Invalid, "unexpected frame")),
		fault.New(fault.Closed, "subscription connection is closing"),
	)
	if !errors.Is(err, context.Canceled) || flaky.calls != 3 || len(gaps) != 1 || gaps[0].Attempts != 2 {
		t.Fatal("establishment outage ended supervision", err, flaky.calls, gaps)
	}
	// The explicit shutdown sentinel stops supervision at once.
	flaky, gaps, err = supervise(pubsub.ErrClosed)
	if !errors.Is(err, pubsub.ErrClosed) || flaky.calls != 1 || len(gaps) != 0 {
		t.Fatal("shutdown was retried", err, flaky.calls, gaps)
	}
	// Invalid input without a disconnect remains permanent.
	flaky, _, err = supervise(fault.New(fault.Invalid, "invalid channel"))
	if !errors.Is(err, fault.Invalid) || flaky.calls != 1 {
		t.Fatal("invalid input was retried", err, flaky.calls)
	}
}
