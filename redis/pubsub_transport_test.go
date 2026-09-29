package redis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/pubsub"
)

func pubsubTestChannel(t *testing.T) pubsub.Channel {
	t.Helper()
	c, err := pubsub.NewChannel(keyspace.Namespace{Application: "test", Environment: "protocol"}, "events", 1, "key")
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func subscribeReply(conn net.Conn, channel string, count int) {
	fmt.Fprintf(conn, "*3\r\n$9\r\nsubscribe\r\n$%d\r\n%s\r\n:%d\r\n", len(channel), channel, count)
}
func TestPubSubEstablishmentRequiresAcknowledgementAndCancels(t *testing.T) {
	entered := make(chan struct{})
	var once sync.Once
	config := transportServer(t, func(conn net.Conn, args []string, _ <-chan struct{}) {
		if strings.EqualFold(args[0], "subscribe") {
			once.Do(func() { close(entered) })
			return
		}
		io.WriteString(conn, "+PONG\r\n")
	})
	c := preparedTransport(t, config)
	if err := c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		s, err := c.Subscribe(ctx, []pubsub.Channel{pubsubTestChannel(t)}, pubsub.DefaultLimits())
		if s != nil {
			s.Close(context.Background())
		}
		finished <- err
	}()
	<-entered
	select {
	case err := <-finished:
		t.Fatal("returned without acknowledgement", err)
	default:
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("setup did not cancel")
	}
	if c.Stats().Subscriptions != 0 || c.Stats().SubscriptionConnections != 0 {
		t.Fatal(c.Stats())
	}
}
func TestPubSubLostPublicationReplyIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	config := transportServer(t, func(conn net.Conn, args []string, _ <-chan struct{}) {
		if strings.EqualFold(args[0], "publish") {
			calls.Add(1)
			conn.Close()
			return
		}
		io.WriteString(conn, "+PONG\r\n")
	})
	c := preparedTransport(t, config)
	if err := c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n, err := c.Publish(t.Context(), pubsubTestChannel(t), []byte("payload")); err == nil || n != 0 || calls.Load() != 1 {
		t.Fatal(n, err, calls.Load())
	}
}
func TestPubSubInterruptedStreamReportsLoss(t *testing.T) {
	var count atomic.Int32
	config := transportServer(t, func(conn net.Conn, args []string, _ <-chan struct{}) {
		if strings.EqualFold(args[0], "subscribe") {
			if count.Add(1) == 1 {
				subscribeReply(conn, args[1], 1)
			}
			conn.Close()
			return
		}
		io.WriteString(conn, "+PONG\r\n")
	})
	c := preparedTransport(t, config)
	if err := c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	sub, err := c.Subscribe(t.Context(), []pubsub.Channel{pubsubTestChannel(t)}, pubsub.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	awaitSubscription(t, sub)
	if !errors.Is(sub.Err(), pubsub.ErrDisconnected) {
		t.Fatal(sub.Err())
	}
	if c.Stats().Subscriptions != 0 || c.Stats().SubscriptionConnections != 0 {
		t.Fatal(c.Stats())
	}
}
func TestPubSubMissingPongTerminatesIdleStream(t *testing.T) {
	config := transportServer(t, func(conn net.Conn, args []string, _ <-chan struct{}) {
		if strings.EqualFold(args[0], "subscribe") {
			subscribeReply(conn, args[1], 1)
			return
		}
		if len(args) == 1 {
			io.WriteString(conn, "+PONG\r\n")
		}
	})
	config.OperationTimeout = 100 * time.Millisecond
	c := preparedTransport(t, config)
	if err := c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	sub, err := c.Subscribe(t.Context(), []pubsub.Channel{pubsubTestChannel(t)}, pubsub.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	awaitSubscription(t, sub)
	if !errors.Is(sub.Err(), pubsub.ErrDisconnected) {
		t.Fatal(sub.Err())
	}
}
func TestPubSubConnectionLossReachesEveryStream(t *testing.T) {
	var mu sync.Mutex
	counts := make(map[net.Conn]int)
	config := transportServer(t, func(conn net.Conn, args []string, _ <-chan struct{}) {
		if strings.EqualFold(args[0], "subscribe") {
			mu.Lock()
			defer mu.Unlock()
			for _, channel := range args[1:] {
				counts[conn]++
				subscribeReply(conn, channel, counts[conn])
			}
			return
		}
		// Heartbeat PINGs carry a payload and are never answered.
		if len(args) == 1 {
			io.WriteString(conn, "+PONG\r\n")
		}
	})
	config.OperationTimeout = 100 * time.Millisecond
	c := preparedTransport(t, config)
	if err := c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	a, err := pubsub.NewChannel(keyspace.Namespace{Application: "test", Environment: "protocol"}, "events", 1, "a")
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.Subscribe(t.Context(), []pubsub.Channel{pubsubTestChannel(t)}, pubsub.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.Subscribe(t.Context(), []pubsub.Channel{a}, pubsub.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if stats := c.Stats(); stats.SubscriptionConnections != 1 {
		t.Fatal(stats)
	}
	for _, s := range []pubsub.Stream{first, second} {
		awaitSubscription(t, s)
		if !errors.Is(s.Err(), pubsub.ErrDisconnected) {
			t.Fatal("stream missed the shared connection gap", s.Err())
		}
	}
	if c.Stats().Subscriptions != 0 || c.Stats().SubscriptionConnections != 0 {
		t.Fatal(c.Stats())
	}
}

// Supervision survives a Redis outage during establishment: a protocol anomaly
// on the shared connection and a connection dropped while subscribing are both
// retryable disconnects, reported through one gap once delivery resumes.
func TestSupervisionResubscribesAcrossEstablishmentOutage(t *testing.T) {
	var attempts atomic.Int32
	config := transportServer(t, func(conn net.Conn, args []string, _ <-chan struct{}) {
		if strings.EqualFold(args[0], "subscribe") {
			switch attempts.Add(1) {
			case 1:
				// An unexpected frame (a message for an unsubscribed channel).
				fmt.Fprintf(conn, "*3\r\n$7\r\nmessage\r\n$5\r\nother\r\n$1\r\nx\r\n")
			case 2:
				conn.Close() // the server went away mid-subscription
			default:
				subscribeReply(conn, args[1], 1)
				payload := `"resumed"`
				fmt.Fprintf(conn, "*3\r\n$7\r\nmessage\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n", len(args[1]), args[1], len(payload), payload)
			}
			return
		}
		if len(args) == 1 {
			io.WriteString(conn, "+PONG\r\n")
		}
	})
	c := preparedTransport(t, config)
	if err := c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	broker, err := pubsub.NewBroker(c, pubsub.DefaultConfig(keyspace.Namespace{Application: "test", Environment: "protocol"}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { broker.Close(context.Background()) })
	topic, err := pubsub.Define[string, string]("supervised", 1, keyspace.StringKeys[string]()).Bind(broker)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var gaps []pubsub.Gap
	var received []string
	err = topic.Supervise(ctx, "key", pubsub.SupervisePolicy{InitialBackoff: time.Millisecond, MaxBackoff: 5 * time.Millisecond},
		func(_ context.Context, value string) error {
			received = append(received, value)
			cancel()
			return nil
		},
		func(_ context.Context, gap pubsub.Gap) error {
			gaps = append(gaps, gap)
			return nil
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("supervision stopped during the outage", err)
	}
	// Two failed attempts, unless the driver's own reconnect of the failed
	// connection consumed the second scripted failure.
	if len(gaps) != 1 || gaps[0].Attempts < 1 || gaps[0].Attempts > 2 || !errors.Is(gaps[0].Cause, pubsub.ErrDisconnected) {
		t.Fatal("outage was not reported as one gap", gaps)
	}
	// The driver may also resubscribe a dying connection; at least three SUBSCRIBE
	// commands reached the server.
	if len(received) != 1 || received[0] != "resumed" || attempts.Load() < 3 {
		t.Fatal(received, attempts.Load())
	}
}
