package publisher_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/outboxstore"
	"github.com/weiloon1234/Foundry-Go/internal/outboxtest"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func appendPublicationAt(t *testing.T, writer outboxtest.Writer, address outboxstore.Address, after time.Time) outboxstore.Message {
	t.Helper()
	instant, err := temporal.NewDateTime(after)
	if err != nil {
		t.Fatal(err)
	}
	var row outboxstore.Message
	if err := writer.Transaction(t.Context(), func(tx *database.Tx) error {
		var err error
		row, err = outboxstore.Append(t.Context(), tx, address, `{}`, attribution.Origin{})
		if err != nil {
			return err
		}
		_, err = outboxstore.QueryFoundryOutbox().Update(t.Context(), tx, row.ID, outboxstore.MessageDraft{}.SetPublishAfter(instant))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return row
}

func readPublication(t *testing.T, writer outboxtest.Writer, address outboxstore.Address, id model.ID[outboxstore.Message]) outboxstore.Message {
	t.Helper()
	var row outboxstore.Message
	if err := writer.Transaction(t.Context(), func(tx *database.Tx) error {
		found, err := outboxstore.Find(t.Context(), tx, address, id)
		if err != nil {
			return err
		}
		var ok bool
		row, ok = found.Get()
		if !ok {
			return errors.New("publication missing after transaction")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return row
}

func TestPostgresPublisherNanosecondClock(t *testing.T) {
	writer := outboxtest.Open(t)
	base := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	source := testkit.NewClock(base.Add(175 * time.Nanosecond))
	finished := base.Add(2*time.Second + 123456789*time.Nanosecond)
	address := outboxstore.Address{Kind: "precision", Destination: "completion", Name: "notice", Version: 1}
	calls := 0
	config := publisher.DefaultConfig()
	config.Clock = source
	p, err := publisher.New(writer, config, publisher.Route{Kind: address.Kind, Destination: address.Destination, Publish: func(context.Context, publisher.Message) error {
		calls++
		source.Set(finished)
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := p.PublishOne(t.Context()); err != nil || result.Found || calls != 0 {
		t.Fatal("empty nanosecond poll failed", err)
	}
	stored := appendPublicationAt(t, writer, address, base.Add(time.Microsecond))
	// Rounding eligibility up would select this row before its stored deadline.
	source.Set(base.Add(999 * time.Nanosecond))
	if result, err := p.PublishOne(t.Context()); err != nil || result.Found || calls != 0 {
		t.Fatal("publication became eligible early", err)
	}
	source.Set(base.Add(1375 * time.Nanosecond))
	result, err := p.PublishOne(t.Context())
	if err != nil || !result.Committed || !result.Found || result.State != outbox.Published || result.Attempts != 1 || calls != 1 {
		t.Fatal("nanosecond completion was not committed", err)
	}
	row := readPublication(t, writer, address, stored.ID)
	completion, present := row.PublishedAt.Get()
	if row.PublishState != outbox.Published || row.PublishAttempts != 1 || !present || !completion.UTC().Equal(base.Add(2*time.Second+123456*time.Microsecond)) {
		t.Fatal("incorrect stored completion precision")
	}
	if !source.Now().Equal(finished) {
		t.Fatal("publisher altered the application clock")
	}
	if result, err := p.PublishOne(t.Context()); err != nil || result.Found || calls != 1 {
		t.Fatal("committed publication was replayed", err)
	}
}

func TestPostgresPublisherRetryDeadlinePrecision(t *testing.T) {
	writer := outboxtest.Open(t)
	base := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name                     string
		fraction, delay, rounded time.Duration
	}{
		{"whole_delay", 175 * time.Nanosecond, time.Second, time.Second + time.Microsecond},
		{"fractional_delay", 175 * time.Nanosecond, 1500 * time.Nanosecond, 2 * time.Microsecond},
		{"one_nanosecond", 175 * time.Nanosecond, time.Nanosecond, time.Microsecond},
		{"aligned_deadline", 175 * time.Nanosecond, 825 * time.Nanosecond, time.Microsecond},
		{"second_rollover", time.Second - 200*time.Nanosecond, 350 * time.Nanosecond, time.Second + time.Microsecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			address := outboxstore.Address{Kind: "precision", Destination: outbox.Destination(test.name), Name: "notice", Version: 1}
			stored := appendPublicationAt(t, writer, address, base)
			source := testkit.NewClock(base.Add(175 * time.Nanosecond))
			finished := base.Add(5*time.Second + test.fraction)
			deadline := finished.Add(test.delay)
			expected := base.Add(5*time.Second + test.rounded)
			completed := expected.Add(123456789 * time.Nanosecond)
			failure := errors.New("transient publication failure")
			calls := 0
			config := publisher.DefaultConfig()
			config.Clock, config.RetryDelay, config.Jitter = source, test.delay, 0
			p, err := publisher.New(writer, config, publisher.Route{Kind: address.Kind, Destination: address.Destination, Publish: func(context.Context, publisher.Message) error {
				calls++
				if calls == 1 {
					source.Set(finished)
					return failure
				}
				source.Set(completed)
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			first, err := p.PublishOne(t.Context())
			if err != nil || !first.Committed || !first.Found || first.State != outbox.Pending || first.Attempts != 1 || !errors.Is(first.Failure, failure) {
				t.Fatal("transient retry was not committed", err)
			}
			row := readPublication(t, writer, address, stored.ID)
			if row.PublishState != outbox.Pending || row.PublishAttempts != 1 || row.PublishReason != "transient" || !row.PublishAfter.UTC().Equal(expected) || !row.PublishedAt.IsNull() {
				t.Fatal("retry timestamp or state differs from committed deadline")
			}
			if gap := expected.Sub(deadline); gap < 0 || gap >= time.Microsecond {
				t.Fatal("retry precision changed the delay beyond its SQL boundary")
			}
			polls := []time.Time{deadline.Add(-time.Nanosecond), expected.Add(-time.Nanosecond)}
			if deadline.Before(expected) {
				polls = append(polls, deadline)
			}
			for _, instant := range polls {
				source.Set(instant)
				if result, err := p.PublishOne(t.Context()); err != nil || result.Found || calls != 1 {
					t.Fatal("retry published before its persisted deadline", err)
				}
			}
			source.Set(expected)
			second, err := p.PublishOne(t.Context())
			if err != nil || !second.Committed || second.State != outbox.Published || second.ID != first.ID || second.Attempts != 2 || calls != 2 {
				t.Fatal("retry not accepted at the exact stored deadline", err)
			}
			row = readPublication(t, writer, address, stored.ID)
			completion, present := row.PublishedAt.Get()
			if row.PublishState != outbox.Published || row.PublishAttempts != 2 || row.PublishReason != "" || !present || !completion.UTC().Equal(completed.Truncate(time.Microsecond)) {
				t.Fatal("retry completion did not retain SQL precision or clear failure state")
			}
			if result, err := p.PublishOne(t.Context()); err != nil || result.Found || calls != 2 {
				t.Fatal("accepted retry published again", err)
			}
		})
	}
}

func TestPostgresPublisherNanosecondCancellationRollsBack(t *testing.T) {
	writer := outboxtest.Open(t)
	base := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	address := outboxstore.Address{Kind: "precision", Destination: "cancellation", Name: "notice", Version: 1}
	stored := appendPublicationAt(t, writer, address, base)
	source := testkit.NewClock(base.Add(175 * time.Nanosecond))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	config := publisher.DefaultConfig()
	config.Clock, config.MaxInFlight = source, 1
	p, err := publisher.New(writer, config, publisher.Route{Kind: address.Kind, Destination: address.Destination, Publish: func(ctx context.Context, _ publisher.Message) error {
		calls++
		if calls == 1 {
			cancel()
			return ctx.Err()
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := p.PublishOne(ctx); err == nil || result.Committed || calls != 1 {
		t.Fatal("cancelled publication claimed commit or failed before delivery")
	}
	row := readPublication(t, writer, address, stored.ID)
	if row.PublishState != outbox.Pending || row.PublishAttempts != 0 || !row.PublishAfter.UTC().Equal(base) || !row.PublishedAt.IsNull() {
		t.Fatal("cancelled transaction retained publication changes")
	}
	if result, err := p.PublishOne(t.Context()); err != nil || !result.Committed || result.State != outbox.Published || result.Attempts != 1 || calls != 2 {
		t.Fatal("cancelled publication lost the row, transaction or capacity", err)
	}
}
