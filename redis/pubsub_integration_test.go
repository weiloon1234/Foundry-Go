package redis

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/pubsubtest"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/pubsub"
)

func pubsubFixture(t *testing.T, configure func(*Config)) (*Client, func(string) pubsub.Channel) {
	t.Helper()
	c, namespace, _ := integrationAddresses(t, configure)
	return c, func(logical string) pubsub.Channel {
		channel, err := pubsub.NewChannel(namespace, "pubsub", 1, logical)
		if err != nil {
			t.Fatal(err)
		}
		return channel
	}
}
func awaitSubscription(t *testing.T, s pubsub.Stream) {
	t.Helper()
	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("subscription did not drain")
	}
}
func readPublication(t *testing.T, s pubsub.Stream) pubsub.Message {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	m, err := s.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func TestPubSubSharedContract(t *testing.T) {
	pubsubtest.Run(t, func(t *testing.T) (pubsub.Backend, func(string) pubsub.Channel) { return pubsubFixture(t, nil) })
}
func TestPubSubCrossClientDatabaseAndNamespace(t *testing.T) {
	c, channel := pubsubFixture(t, nil)
	config := integrationConfig(t)
	config.Database = 1
	other, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close(context.Background()) })
	a := channel("same")
	sub, err := other.Subscribe(t.Context(), []pubsub.Channel{a}, pubsub.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sub.Close(context.Background()) })
	if n, err := c.Publish(t.Context(), a, []byte("cross-db")); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if m := readPublication(t, sub); string(m.Data) != "cross-db" {
		t.Fatal(m)
	}
	foreign, err := pubsub.NewChannel(keyspace.Namespace{Application: "other", Environment: a.Namespace().Environment}, "pubsub", 1, "same")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := c.Publish(t.Context(), foreign, []byte("isolated")); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}
func TestPubSubSlowConsumerAndPayloadBounds(t *testing.T) {
	for _, kind := range []string{"messages", "bytes", "payload"} {
		t.Run(kind, func(t *testing.T) {
			c, channel := pubsubFixture(t, nil)
			limits := pubsub.DefaultLimits()
			limits.Messages = 1
			limits.Bytes = 4
			limits.PayloadBytes = 4
			if kind == "bytes" {
				limits.Messages = 4
			}
			a := channel(kind)
			sub, err := c.Subscribe(t.Context(), []pubsub.Channel{a}, limits)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { sub.Close(context.Background()) })
			for i := range 2 {
				data := []byte("abc")
				if kind == "payload" {
					data = []byte("abcde")
				}
				if _, err := c.Publish(t.Context(), a, data); err != nil {
					t.Fatal(err)
				}
				if kind == "payload" || i == 1 {
					break
				}
			}
			awaitSubscription(t, sub)
			want := error(pubsub.ErrOverflow)
			if kind == "payload" {
				want = fault.Invalid
			}
			if !errors.Is(sub.Err(), want) {
				t.Fatal(sub.Err())
			}
			if c.Stats().Subscriptions != 0 || c.Stats().SubscriptionConnections != 0 {
				t.Fatal(c.Stats())
			}
		})
	}
}
func TestPubSubIdleHeartbeatAndClientShutdown(t *testing.T) {
	c, channel := pubsubFixture(t, func(c *Config) { c.OperationTimeout = 100 * time.Millisecond; c.MaxSubscriptions = 1 })
	a := channel("idle")
	sub, err := c.Subscribe(t.Context(), []pubsub.Channel{a}, pubsub.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Subscribe(t.Context(), []pubsub.Channel{channel("overflow")}, pubsub.DefaultLimits()); !errors.Is(err, fault.Conflict) {
		t.Fatal(err)
	}
	// Exercise several real PING/PONG cycles without a publication.
	timer := time.NewTimer(350 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-sub.Done():
		t.Fatal(sub.Err())
	case <-timer.C:
	}
	if n, err := c.Publish(t.Context(), a, []byte("alive")); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	readPublication(t, sub)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := c.Close(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	awaitSubscription(t, sub)
	if stats := c.Stats(); stats.Open != 0 || stats.Operations != 0 || stats.Subscriptions != 0 || stats.SubscriptionConnections != 0 {
		t.Fatal(stats)
	}
}

// The publication reaches the real server; only the publisher's reply is hidden.
type lostPublicationAcknowledgement struct {
	calls   atomic.Int32
	failure error
}

func (h *lostPublicationAcknowledgement) DialHook(next driver.DialHook) driver.DialHook { return next }
func (h *lostPublicationAcknowledgement) ProcessPipelineHook(next driver.ProcessPipelineHook) driver.ProcessPipelineHook {
	return next
}
func (h *lostPublicationAcknowledgement) ProcessHook(next driver.ProcessHook) driver.ProcessHook {
	return func(ctx context.Context, cmd driver.Cmder) error {
		args := cmd.Args()
		matches := len(args) > 0 && args[0] == "publish"
		err := next(ctx, cmd)
		if matches {
			h.calls.Add(1)
			if err == nil {
				return h.failure
			}
		}
		return err
	}
}
func TestPubSubAppliedPublicationWithLostAcknowledgement(t *testing.T) {
	c, channel := pubsubFixture(t, nil)
	a := channel("unknown-publication")
	s, err := c.Subscribe(t.Context(), []pubsub.Channel{a}, pubsub.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(context.Background()) })
	hook := &lostPublicationAcknowledgement{failure: errors.New("lost acknowledgement")}
	c.raw.AddHook(hook)
	if count, err := c.Publish(t.Context(), a, []byte("applied")); count != 0 || !errors.Is(err, hook.failure) || hook.calls.Load() != 1 {
		t.Fatal(count, err, hook.calls.Load())
	}
	if message := readPublication(t, s); string(message.Data) != "applied" {
		t.Fatal(message)
	}
}
