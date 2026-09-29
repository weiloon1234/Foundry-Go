package publisher_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/outboxstore"
	"github.com/weiloon1234/Foundry-Go/internal/outboxtest"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

type cyclicPublicationError struct{ visits int }

func (*cyclicPublicationError) Error() string { panic("publication error must not be formatted") }
func (e *cyclicPublicationError) Unwrap() error {
	e.visits++
	// A finite escape makes the old implementation fail its bound assertion
	// instead of hanging the whole acceptance process.
	if e.visits > 512 {
		return nil
	}
	return e
}

type faultyPublicationError struct{ exit bool }

func (faultyPublicationError) Error() string { panic("publication error must not be formatted") }
func (e faultyPublicationError) Is(error) bool {
	if e.exit {
		runtime.Goexit()
	}
	panic("private classification failure")
}

func TestPostgresPublicationFailureInspectionReleasesRowAndRetainsRetry(t *testing.T) {
	writer := outboxtest.Open(t)
	cycle := &cyclicPublicationError{}
	for _, test := range []struct {
		name      string
		failure   error
		permanent bool
	}{
		{"transient", errors.New("private provider failure"), false},
		{"cycle", cycle, false},
		{"panic", faultyPublicationError{}, false},
		{"goexit", faultyPublicationError{exit: true}, false},
		{"invalid", fmt.Errorf("wrapped: %w", fault.Invalid), true},
		// An unregistered job/event name is transient during rolling deploys.
		{"missing", errors.Join(errors.New("transient"), fault.Missing), false},
		// A queue policy mismatch during a configuration rollout is an operator
		// problem, never a permanent property of the row.
		{"queue-policy", fmt.Errorf("enqueue: %w", jobs.ErrQueuePolicy), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			address := outboxstore.Address{Kind: "inspection", Destination: outbox.Destination(test.name), Name: "notice", Version: 1}
			var stored outboxstore.Message
			if err := writer.Transaction(t.Context(), func(tx *database.Tx) error {
				var err error
				stored, err = outboxstore.Append(t.Context(), tx, address, `{}`, attribution.Origin{})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			config := publisher.DefaultConfig()
			clock := testkit.NewClock(time.Now().Add(time.Second))
			config.Clock = clock
			failure := test.failure
			p, err := publisher.New(writer, config, publisher.Route{
				Kind: address.Kind, Destination: address.Destination,
				Publish: func(context.Context, publisher.Message) error { return failure },
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := p.PublishOne(t.Context())
			expected, reason := outbox.Pending, "transient"
			if test.permanent {
				expected, reason = outbox.PublicationFailed, "permanent"
			}
			if err != nil || !result.Committed || !result.Found || result.State != expected || result.Attempts != 1 || !errorgraph.Is(result.Failure, test.failure) {
				t.Fatal("publication failure lost its committed classification")
			}
			if cycle.visits > 256 {
				t.Fatal("publication inspected an unbounded error chain", cycle.visits)
			}
			// A separate committed read proves the prior publication released
			// its transaction and persisted the retry/permanent decision.
			if err := writer.Transaction(t.Context(), func(tx *database.Tx) error {
				found, err := outboxstore.Find(t.Context(), tx, address, stored.ID)
				if err != nil {
					return err
				}
				row, ok := found.Get()
				if !ok || row.PublishState != expected || row.PublishReason != reason || row.PublishAttempts != 1 {
					return errors.New("stored publication decision differs")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			failure = nil
			clock.Advance(config.RetryDelay + config.Jitter + time.Microsecond)
			retry, err := p.PublishOne(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if test.permanent {
				if retry.Found {
					t.Fatal("permanent failure was retried")
				}
			} else if !retry.Committed || retry.State != outbox.Published || retry.ID != result.ID || retry.Attempts != 2 {
				t.Fatal("inspection failure stranded a retryable publication")
			}
		})
	}
}

// Observation happens after commit. Its error is logged with a bounded
// diagnostic and never stops Run, strands its ownership, loses that committed
// row, or admits a duplicate publication.
func TestPostgresCancelledObservationReleasesPublisherRun(t *testing.T) {
	writer := outboxtest.Open(t)
	for _, mode := range []string{"cycle", "panic", "goexit", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			cycle := new(cyclicPublicationError)
			var failure error = cycle
			switch mode {
			case "panic":
				failure = faultyPublicationError{}
			case "goexit":
				failure = faultyPublicationError{exit: true}
			case "cancelled":
				failure = fmt.Errorf("observed: %w", context.Canceled)
			}
			address := outboxstore.Address{Kind: "observation", Destination: outbox.Destination(mode), Name: "notice", Version: 1}
			appendMessage := func() outboxstore.Message {
				t.Helper()
				var stored outboxstore.Message
				if err := writer.Transaction(t.Context(), func(tx *database.Tx) error {
					var err error
					stored, err = outboxstore.Append(t.Context(), tx, address, `{}`, attribution.Origin{})
					return err
				}); err != nil {
					t.Fatal(err)
				}
				return stored
			}
			first := appendMessage()
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			published, observed := 0, 0
			config := publisher.DefaultConfig()
			config.MaxInFlight = 1
			config.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
			source := testkit.NewClock(time.Now().Add(time.Second))
			config.Clock = source
			config.Observe = func(_ context.Context, result publisher.Result) error {
				observed++
				if !result.Found || !result.Committed || result.State != outbox.Published || result.Attempts != 1 {
					return errors.New("observer lost committed publication")
				}
				cancel()
				return failure
			}
			p, err := publisher.New(writer, config, publisher.Route{Kind: address.Kind, Destination: address.Destination, Publish: func(context.Context, publisher.Message) error {
				published++
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			var runErr error
			returned := false
			escaped := callback.Isolated("test publisher run", func() error {
				runErr = p.Run(ctx)
				returned = true
				return nil
			})
			if escaped != nil || !returned || runErr != nil {
				t.Fatal("observation error escaped Run or stopped it", runErr)
			}
			if mode == "cycle" && cycle.visits > 256 {
				t.Fatal("observation diagnostic inspection exceeded its bound")
			}
			if published != 1 || observed != 1 {
				t.Fatal("observation replayed or skipped publication")
			}
			if err := writer.Transaction(t.Context(), func(tx *database.Tx) error {
				found, err := outboxstore.Find(t.Context(), tx, address, first.ID)
				if err != nil {
					return err
				}
				row, ok := found.Get()
				if !ok || row.PublishState != outbox.Published || row.PublishAttempts != 1 {
					return errors.New("observation changed committed state")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			appendMessage()
			source.Advance(time.Minute)
			failure = nil
			ctx, cancel = context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			if err := p.Run(ctx); err != nil {
				t.Fatal("publisher Run ownership was not reusable", err)
			}
			if published != 2 || observed != 2 {
				t.Fatal("later run replayed the committed row or lost capacity")
			}
		})
	}
}
