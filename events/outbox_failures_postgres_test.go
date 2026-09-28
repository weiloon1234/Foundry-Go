package events_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/outboxstore"
)

func TestOutboxContainsEncoderFailuresAndReleasesAdmissionWithoutWriting(t *testing.T) {
	within := outboxTransactions(t)
	topic := events.Define[encodingFailure]("test.durable_encoding", 1)
	config := events.DefaultConfig()
	config.MaxInFlight = 1
	bus := startedBus(t, config, declaration(t, topic))
	producer, err := events.PrepareOutbox("application.events", bus)
	if err != nil {
		t.Fatal(err)
	}
	if err := within(t.Context(), func(tx *database.Tx) error {
		for _, input := range []encodingFailure{"panic", "exit", "error"} {
			_, err := topic.Enqueue(t.Context(), tx, producer, input)
			expected := error(fault.Panicked)
			if input == "error" {
				expected = fault.Invalid
			}
			if !errors.Is(err, expected) || strings.Contains(err.Error(), "private") {
				return errors.New("outbox encoder failure escaped or exposed data")
			}
		}
		count, err := outboxstore.QueryFoundryOutbox().Count(t.Context(), tx)
		if err != nil {
			return err
		}
		if count != 0 {
			return errors.New("failed payload capture wrote an outbox row")
		}
		id, err := topic.Enqueue(t.Context(), tx, producer, "valid")
		if err != nil {
			return err
		}
		if id.IsZero() {
			return errors.New("failed capture retained admission or lost a later identity")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCanceledOutboxCaptureWaitsForEncoderExitAndLeavesNoRecord(t *testing.T) {
	within := outboxTransactions(t)
	topic := events.Define[blockedEncoding]("test.durable_cancellation", 1)
	config := events.DefaultConfig()
	config.MaxInFlight = 1
	bus := startedBus(t, config, declaration(t, topic))
	producer, err := events.PrepareOutbox("application.events", bus)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- within(t.Context(), func(tx *database.Tx) error {
			_, err := topic.Enqueue(ctx, tx, producer, blockedEncoding{Value: 1, Entered: entered, Release: release})
			return err
		})
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("outbox encoder did not start")
	}
	cancel()
	select {
	case <-result:
		t.Fatal("enqueue abandoned a running custom encoder")
	default:
	}
	once.Do(func() { close(release) })
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("canceled encoder capture did not fail the transaction", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("encoder exit did not release its transaction")
	}
	if err := within(t.Context(), func(tx *database.Tx) error {
		count, err := outboxstore.QueryFoundryOutbox().Count(t.Context(), tx)
		if err != nil {
			return err
		}
		if count != 0 {
			return errors.New("canceled capture retained an outbox row")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
