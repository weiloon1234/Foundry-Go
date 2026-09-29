package publisher_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/outboxstore"
	"github.com/weiloon1234/Foundry-Go/internal/outboxtest"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func appendPublication(t *testing.T, writer outboxtest.Writer, address outboxstore.Address) outboxstore.Message {
	t.Helper()
	var row outboxstore.Message
	if err := writer.Transaction(t.Context(), func(tx *database.Tx) error {
		var err error
		row, err = outboxstore.Append(t.Context(), tx, address, `{}`, attribution.Origin{})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return row
}

// flakyWriter fails its first transactions like an unavailable database.
type flakyWriter struct {
	outboxtest.Writer
	failures atomic.Int32
}

func (w *flakyWriter) Transaction(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
	if w.failures.Add(-1) >= 0 {
		return errors.New("database unavailable")
	}
	return w.Writer.Transaction(ctx, fn, options...)
}

func TestPostgresPublisherRunSurvivesTransientTransactionFailures(t *testing.T) {
	writer := &flakyWriter{Writer: outboxtest.Open(t)}
	writer.failures.Store(3)
	address := outboxstore.Address{Kind: "survive", Destination: "run", Name: "notice", Version: 1}
	appendPublication(t, writer.Writer, address)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	config := publisher.DefaultConfig()
	config.PollInterval, config.MaxPollInterval = time.Millisecond, 4*time.Millisecond
	config.Clock = testkit.NewClock(time.Now().Add(time.Second))
	published := 0
	config.Observe = func(_ context.Context, result publisher.Result) error {
		if result.State == outbox.Published {
			cancel()
		}
		return nil
	}
	p, err := publisher.New(writer, config, publisher.Route{Kind: address.Kind, Destination: address.Destination, Publish: func(context.Context, publisher.Message) error {
		published++
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Run(ctx); err != nil {
		t.Fatal("transient failures stopped Run", err)
	}
	if published != 1 || writer.failures.Load() >= 0 {
		t.Fatal("publisher did not keep running through transaction failures", published)
	}
}

func TestPostgresPublisherBatchesAcrossRoutesAndBacksOffExponentially(t *testing.T) {
	writer := outboxtest.Open(t)
	first := outboxstore.Address{Kind: "batch", Destination: "first", Name: "notice", Version: 1}
	second := outboxstore.Address{Kind: "batch", Destination: "second", Name: "notice", Version: 1}
	var ids []model.ID[outboxstore.Message]
	for _, address := range []outboxstore.Address{first, second, first} {
		ids = append(ids, appendPublication(t, writer, address).ID)
	}
	base := time.Now().Add(time.Second).Truncate(time.Second)
	source := testkit.NewClock(base)
	config := publisher.DefaultConfig()
	config.Clock, config.Jitter, config.MaxInFlight = source, 0, 4
	failing := true
	publish := func(context.Context, publisher.Message) error {
		if failing {
			return errors.New("broker unavailable")
		}
		return nil
	}
	p, err := publisher.New(writer, config, publisher.Route{Kind: first.Kind, Destination: first.Destination, Publish: publish}, publisher.Route{Kind: second.Kind, Destination: second.Destination, Publish: publish})
	if err != nil {
		t.Fatal(err)
	}
	// Two failed attempts: the second waits twice RetryDelay.
	for attempt, delay := range []time.Duration{config.RetryDelay, 2 * config.RetryDelay} {
		results, err := p.PublishBatch(t.Context())
		if err != nil || len(results) != 3 {
			t.Fatal("one claim did not cover every route", len(results), err)
		}
		row := readPublication(t, writer, first, ids[0])
		if row.PublishState != outbox.Pending || row.PublishAttempts != uint32(attempt+1) || !row.PublishAfter.UTC().Equal(source.Now().Add(delay)) {
			t.Fatal("retry deadline is not exponential", attempt, row.PublishAfter.UTC(), source.Now().Add(delay))
		}
		source.Advance(delay)
	}
	failing = false
	results, err := p.PublishBatch(t.Context())
	if err != nil || len(results) != 3 {
		t.Fatal(len(results), err)
	}
	for _, result := range results {
		if !result.Committed || result.State != outbox.Published || result.Attempts != 3 {
			t.Fatalf("batch publication: %+v", result)
		}
	}
}

func TestPostgresPublisherRequeuesFailedAndPrunesPublished(t *testing.T) {
	writer := outboxtest.Open(t)
	job := outboxstore.Address{Kind: "job", Destination: "default", Name: "notice", Version: 1}
	event := outboxstore.Address{Kind: "event", Destination: "default", Name: "notice", Version: 1}
	failedJob := appendPublication(t, writer, job)
	failedEvent := appendPublication(t, writer, event)
	source := testkit.NewClock(time.Now().Add(time.Second))
	config := publisher.DefaultConfig()
	config.Clock, config.MaxAttempts = source, 1
	failing := true
	publish := func(context.Context, publisher.Message) error {
		if failing {
			return errors.New("broker unavailable")
		}
		return nil
	}
	p, err := publisher.New(writer, config, publisher.Route{Kind: job.Kind, Destination: job.Destination, Publish: publish}, publisher.Route{Kind: event.Kind, Destination: event.Destination, Publish: publish})
	if err != nil {
		t.Fatal(err)
	}
	if results, err := p.PublishBatch(t.Context()); err != nil || len(results) != 2 {
		t.Fatal(len(results), err)
	}
	stats, err := p.Stats(t.Context())
	if err != nil || stats != (publisher.Stats{Failed: 2}) {
		t.Fatalf("stats: %+v %v", stats, err)
	}
	failures, err := p.Failed(t.Context(), publisher.Selection{Kind: "job", Limit: 10})
	if err != nil || len(failures) != 1 || failures[0].ID != model.IDFromBytes[publisher.Publication](failedJob.ID.Bytes()) || failures[0].Reason != "attempt_limit" {
		t.Fatalf("failed listing: %+v %v", failures, err)
	}
	if changed, err := p.Requeue(t.Context(), publisher.Selection{Kind: "job"}); err != nil || changed != 1 {
		t.Fatal("requeue by kind", changed, err)
	}
	if row := readPublication(t, writer, event, failedEvent.ID); row.PublishState != outbox.PublicationFailed {
		t.Fatal("requeue by kind changed another kind")
	}
	failing = false
	if results, err := p.PublishBatch(t.Context()); err != nil || len(results) != 1 || results[0].State != outbox.Published {
		t.Fatal("requeued row was not published", err)
	}
	if changed, err := p.Requeue(t.Context(), publisher.Selection{ID: model.IDFromBytes[publisher.Publication](failedEvent.ID.Bytes())}); err != nil || changed != 1 {
		t.Fatal("requeue by ID", changed, err)
	}
	if results, err := p.PublishBatch(t.Context()); err != nil || len(results) != 1 {
		t.Fatal(err)
	}
	// Published rows older than the cutoff are pruned in bounded batches.
	if deleted, err := p.Prune(t.Context(), time.Now().Add(-time.Hour), 10); err != nil || deleted != 0 {
		t.Fatal("prune removed recent rows", deleted, err)
	}
	if deleted, err := p.Prune(t.Context(), source.Now().Add(time.Hour), 1); err != nil || deleted != 1 {
		t.Fatal("prune batch", deleted, err)
	}
	if deleted, err := p.Prune(t.Context(), source.Now().Add(time.Hour), 10); err != nil || deleted != 1 {
		t.Fatal("prune remainder", deleted, err)
	}
	if stats, err := p.Stats(t.Context()); err != nil || stats != (publisher.Stats{}) {
		t.Fatalf("stats after prune: %+v %v", stats, err)
	}
}

// A publish that uses its whole deadline must not roll back its batch: the
// co-claimed successes stay published and the slow row's attempt is counted.
func TestPostgresPublisherSlowRouteDoesNotRollBackItsBatch(t *testing.T) {
	writer := outboxtest.Open(t)
	fast := outboxstore.Address{Kind: "slow", Destination: "fast", Name: "notice", Version: 1}
	slow := outboxstore.Address{Kind: "slow", Destination: "slow", Name: "notice", Version: 1}
	appendPublication(t, writer, fast)
	slowRow := appendPublication(t, writer, slow)
	appendPublication(t, writer, fast)
	source := testkit.NewClock(time.Now().Add(time.Second))
	config := publisher.DefaultConfig()
	config.Clock, config.Jitter, config.MaxInFlight, config.OperationTimeout = source, 0, 4, 200*time.Millisecond
	var fastCalls, slowCalls atomic.Int32
	p, err := publisher.New(writer, config,
		publisher.Route{Kind: fast.Kind, Destination: fast.Destination, Publish: func(context.Context, publisher.Message) error { fastCalls.Add(1); return nil }},
		publisher.Route{Kind: slow.Kind, Destination: slow.Destination, Publish: func(ctx context.Context, _ publisher.Message) error {
			slowCalls.Add(1)
			<-ctx.Done()
			return ctx.Err()
		}})
	if err != nil {
		t.Fatal(err)
	}
	results, err := p.PublishBatch(t.Context())
	if err != nil || len(results) != 3 {
		t.Fatal("slow publication rolled back its batch", len(results), err)
	}
	for _, result := range results {
		if !result.Committed {
			t.Fatal("batch outcome not committed")
		}
	}
	row := readPublication(t, writer, slow, slowRow.ID)
	if row.PublishState != outbox.Pending || row.PublishAttempts != 1 || row.PublishReason != "transient" {
		t.Fatal("timed-out attempt was not recorded", row.PublishState, row.PublishAttempts)
	}
	source.Advance(config.RetryDelay + time.Millisecond)
	if results, err := p.PublishBatch(t.Context()); err != nil || len(results) != 1 || results[0].Attempts != 2 {
		t.Fatal("only the slow row should be claimed again", len(results), err)
	}
	if fastCalls.Load() != 2 || slowCalls.Load() != 2 {
		t.Fatal("successful rows were republished", fastCalls.Load(), slowCalls.Load())
	}
}
