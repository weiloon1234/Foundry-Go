package pubsub_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/pubsub"
	"github.com/weiloon1234/Foundry-Go/pubsub/memory"
)

type receiveClassificationError struct{ exit bool }

func (receiveClassificationError) Error() string { return "private stream error" }
func (e receiveClassificationError) Is(error) bool {
	if e.exit {
		runtime.Goexit()
	}
	panic("private stream panic")
}

type failingReceiveBackend struct {
	pubsub.Backend
	cancel  context.CancelFunc
	failure error
}
type failingReceiveStream struct {
	pubsub.Stream
	cancel  context.CancelFunc
	failure error
}

func (b failingReceiveBackend) Subscribe(ctx context.Context, channels []pubsub.Channel, limits pubsub.Limits) (pubsub.Stream, error) {
	stream, err := b.Backend.Subscribe(ctx, channels, limits)
	if err != nil {
		return nil, err
	}
	return failingReceiveStream{stream, b.cancel, b.failure}, nil
}
func (s failingReceiveStream) Next(context.Context) (pubsub.Message, error) {
	s.cancel()
	return pubsub.Message{}, s.failure
}

func TestCanceledReceiveErrorInspectionCannotEscape(t *testing.T) {
	for _, exit := range []bool{false, true} {
		raw, err := memory.New(4)
		if err != nil {
			t.Fatal(err)
		}
		defer raw.Close()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		broker, err := pubsub.NewBroker(failingReceiveBackend{raw, cancel, receiveClassificationError{exit}}, pubsub.DefaultConfig(keyspace.Namespace{Application: "test", Environment: "inspection"}))
		if err != nil {
			t.Fatal(err)
		}
		defer broker.Close(context.Background())
		topic, err := changed.Bind(broker)
		if err != nil {
			t.Fatal(err)
		}
		stream, err := topic.Subscribe(t.Context(), 1)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { _, err := stream.Receive(ctx); done <- err }()
		select {
		case err := <-done:
			if !errors.Is(err, fault.Panicked) {
				t.Fatal("inspection failure not classified", err)
			}
		case <-time.After(time.Second):
			t.Fatal("inspection escaped receive")
		}
		waitDone(t, stream.Done())
		if broker.Stats().Subscriptions != 0 {
			t.Fatal("failed receiver retained subscription")
		}
	}
}

// The escape keeps a regression against the old unbounded traversal finite.
type cyclicReceiveError struct{ visits int }

func (*cyclicReceiveError) Error() string { panic("private stream error must not be formatted") }
func (e *cyclicReceiveError) Unwrap() error {
	e.visits++
	if e.visits > 512 {
		return nil
	}
	return e
}

func TestCanceledReceiveBoundsInspectionAndPreservesWrappedCancellation(t *testing.T) {
	cycle := &cyclicReceiveError{}
	for _, test := range []struct {
		name    string
		failure error
		keep    bool
	}{
		{"cycle", cycle, false},
		{"wrapped", fmt.Errorf("receive: %w", context.Canceled), true},
		{"joined", errors.Join(errors.New("stream detail"), context.Canceled), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := memory.New(4)
			if err != nil {
				t.Fatal(err)
			}
			defer raw.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			broker, err := pubsub.NewBroker(failingReceiveBackend{raw, cancel, test.failure}, pubsub.DefaultConfig(keyspace.Namespace{Application: "test", Environment: "bounded-inspection"}))
			if err != nil {
				t.Fatal(err)
			}
			defer broker.Close(context.Background())
			topic, err := changed.Bind(broker)
			if err != nil {
				t.Fatal(err)
			}
			stream, err := topic.Subscribe(t.Context(), 1)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := stream.Receive(ctx); !errorgraph.Is(err, test.failure) {
				t.Fatal("receive did not preserve the original stream failure")
			}
			if cycle.visits > 256 {
				t.Fatal("cyclic error exceeded inspection bound", cycle.visits)
			}
			if test.keep {
				if stream.Err() != nil || broker.Stats().Subscriptions != 1 {
					t.Fatal("wrapped cancellation discarded a live subscription")
				}
			} else {
				waitDone(t, stream.Done())
				if !errorgraph.Is(stream.Err(), test.failure) || broker.Stats().Subscriptions != 0 {
					t.Fatal("unclassifiable failure did not terminate and release the subscription")
				}
			}
			if err := stream.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := broker.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			waitDone(t, broker.Done())
		})
	}
}
