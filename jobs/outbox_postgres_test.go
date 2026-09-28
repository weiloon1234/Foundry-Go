package jobs_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/outboxtest"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

// Deliberate fault-injection adapter; real durable Redis is tested separately.
type durablePeer struct {
	*memory.Backend
	loseReply atomic.Bool
	accepted  atomic.Int32
}

func (*durablePeer) DurableAcceptance() bool { return true }
func (p *durablePeer) JobEnqueue(ctx context.Context, key jobs.Key, e jobs.Envelope) (bool, error) {
	inserted, err := p.Backend.JobEnqueue(ctx, key, e)
	if err != nil {
		return inserted, err
	}
	if inserted {
		p.accepted.Add(1)
	}
	if p.loseReply.Swap(false) {
		return false, errors.New("accepted but response lost")
	}
	return inserted, nil
}
func TestJobOutboxRollbackAndAmbiguousPublication(t *testing.T) {
	writer := outboxtest.Open(t)
	backend, err := memory.New(memory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	peer := &durablePeer{Backend: backend}
	definition := jobs.Define[payload]("outbox.work", 1, jobs.DefaultPolicy("default"))
	declaration, err := definition.Declare(nil)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := jobs.NewDispatcher(peer, registry, jobs.DefaultDispatchConfig(keyspace.Namespace{Application: "publication", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	producer, err := jobs.PrepareOutbox("jobs", dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	route, err := producer.PublicationRoute()
	if err != nil {
		t.Fatal(err)
	}
	config := publisher.DefaultConfig()
	clock := testkit.NewClock(time.Now().Add(time.Second).Truncate(time.Microsecond).Add(175 * time.Nanosecond))
	config.Clock = clock
	publisher, err := publisher.New(writer, config, route)
	if err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("rollback")
	if err := writer.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := definition.Enqueue(t.Context(), tx, producer, payload{Labels: map[string]string{}}, jobs.Options[payload]{}); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if result, err := publisher.PublishOne(t.Context()); err != nil || result.Found {
		t.Fatal(result, err)
	}
	trace, err := tracing.New(true)
	if err != nil {
		t.Fatal(err)
	}
	traceContext, err := tracing.WithContext(t.Context(), trace)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := definition.Capture(traceContext, payload{Labels: map[string]string{"snapshot": "original"}}, jobs.Options[payload]{PropagateTrace: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Transaction(t.Context(), func(tx *database.Tx) error { _, err := pending.Enqueue(t.Context(), tx, producer); return err }); err != nil {
		t.Fatal(err)
	}
	peer.loseReply.Store(true)
	first, err := publisher.PublishOne(t.Context())
	if err != nil || !first.Committed || first.State != outbox.Pending || first.Failure == nil {
		t.Fatal(first, err)
	}
	// Publication deadlines round up to SQL precision, never before the full
	// delay measured from the original nanosecond clock sample.
	clock.Advance(config.RetryDelay + time.Microsecond)
	second, err := publisher.PublishOne(t.Context())
	if err != nil || !second.Committed || second.State != outbox.Published || second.ID != first.ID || second.Attempts != 2 {
		t.Fatal(second, err)
	}
	if peer.accepted.Load() != 1 {
		t.Fatal("ambiguous publication duplicated queue identity")
	}
	found, err := definition.Inspect(t.Context(), dispatcher, pending.ID(), "")
	if err != nil || !found.IsSet() {
		t.Fatal("stable execution ID lost", err)
	}
	record, _ := found.Get()
	propagated, present := record.Envelope.Trace().Get()
	if !present || propagated.TraceParent() != trace.TraceParent() || record.Envelope.WireVersion() != jobs.TracedEnvelope {
		t.Fatal("outbox retry lost captured trace envelope")
	}
	third, err := publisher.PublishOne(t.Context())
	if err != nil || third.Found {
		t.Fatal("published row selected again", err)
	}
}
func TestMemoryOutboxPublicationIsRejected(t *testing.T) {
	f := newWorkerFixture(t, jobs.DefaultPolicy("default"), nil)
	producer, err := jobs.PrepareOutbox("jobs", f.dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := producer.PublicationRoute(); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
func TestConcurrentOutboxPublishersSkipLockedRows(t *testing.T) {
	writer := outboxtest.Open(t)
	backend, err := memory.New(memory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	peer := &durablePeer{Backend: backend}
	d := jobs.Define[payload]("concurrent.work", 1, jobs.DefaultPolicy("default"))
	decl, err := d.Declare(nil)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(decl)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := jobs.NewDispatcher(peer, registry, jobs.DefaultDispatchConfig(keyspace.Namespace{Application: "concurrent", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	producer, err := jobs.PrepareOutbox("jobs", dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Transaction(t.Context(), func(tx *database.Tx) error {
		for range 8 {
			if _, err := d.Enqueue(t.Context(), tx, producer, payload{Labels: map[string]string{}}, jobs.Options[payload]{}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	route, err := producer.PublicationRoute()
	if err != nil {
		t.Fatal(err)
	}
	config := publisher.DefaultConfig()
	config.Clock = testkit.NewClock(time.Now().Add(time.Second))
	p, err := publisher.New(writer, config, route)
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	var published atomic.Int32
	for range 8 {
		group.Go(func() {
			result, err := p.PublishOne(t.Context())
			if err != nil {
				t.Error(err)
				return
			}
			if result.State == outbox.Published {
				published.Add(1)
			}
		})
	}
	group.Wait()
	if published.Load() != 8 || peer.accepted.Load() != 8 {
		t.Fatal("publication concurrency lost or duplicated work", published.Load(), peer.accepted.Load())
	}
}
