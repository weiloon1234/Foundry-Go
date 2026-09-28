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
