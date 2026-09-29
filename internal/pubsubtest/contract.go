// Package pubsubtest owns shared live fan-out acceptance for memory and Redis.
package pubsubtest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/pubsub"
)

func Run(t *testing.T, factory func(*testing.T) (pubsub.Backend, func(string) pubsub.Channel)) {
	t.Helper()
	t.Run("readiness-fanout-and-exact-channels", func(t *testing.T) {
		b, channel := factory(t)
		a, z := channel("a"), channel("z")
		both, err := b.Subscribe(t.Context(), []pubsub.Channel{a, z}, pubsub.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { both.Close(context.Background()) })
		one, err := b.Subscribe(t.Context(), []pubsub.Channel{a}, pubsub.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { one.Close(context.Background()) })
		data := []byte("original")
		// The count is the authority's: memory counts subscriptions, while Redis
		// counts subscribed connections and one client multiplexes its streams.
		if n, err := b.Publish(t.Context(), a, data); err != nil || n == 0 || n > 2 {
			t.Fatal(n, err)
		}
		data[0] = 'X'
		first := next(t, both)
		first.Data[0] = 'Y'
		second := next(t, one)
		if string(second.Data) != "original" || second.Channel != a {
			t.Fatal(second)
		}
		if n, err := b.Publish(t.Context(), z, []byte("second")); err != nil || n != 1 {
			t.Fatal(n, err)
		}
		if m := next(t, both); m.Channel != z || string(m.Data) != "second" {
			t.Fatal(m)
		}
		if n, err := b.Publish(t.Context(), channel("absent"), []byte("none")); err != nil || n != 0 {
			t.Fatal(n, err)
		}
	})
	t.Run("cancellation-and-close", func(t *testing.T) {
		b, channel := factory(t)
		a := channel("cancel")
		s, err := b.Subscribe(t.Context(), []pubsub.Channel{a}, pubsub.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close(context.Background()) })
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := s.Next(ctx); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, err := b.Publish(t.Context(), a, []byte("later")); err != nil {
			t.Fatal(err)
		}
		if m := next(t, s); string(m.Data) != "later" {
			t.Fatal(m)
		}
		if err := s.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
		select {
		case <-s.Done():
		default:
			t.Fatal("close returned before stream exited")
		}
		if _, err := s.Next(t.Context()); !errors.Is(err, pubsub.ErrClosed) {
			t.Fatal(err)
		}
	})
	t.Run("invalid-setup", func(t *testing.T) {
		b, channel := factory(t)
		a := channel("validation")
		if _, err := b.Subscribe(t.Context(), nil, pubsub.DefaultLimits()); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		if _, err := b.Subscribe(t.Context(), []pubsub.Channel{a, a}, pubsub.DefaultLimits()); !errors.Is(err, fault.Duplicate) {
			t.Fatal(err)
		}
		if _, err := b.Publish(t.Context(), a, nil); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	})
}
func next(t *testing.T, s pubsub.Stream) pubsub.Message {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	m, err := s.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
