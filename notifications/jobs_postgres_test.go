package notifications

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database"
	store "github.com/weiloon1234/Foundry-Go/internal/notificationstore"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
)

// A process-local durability stand-in for this outbox composition test. The jobs
// subsystem separately verifies real Redis persistence and publication recovery.
type durableQueue struct{ *memory.Backend }

func (*durableQueue) DurableAcceptance() bool { return true }

func TestNotificationOutboxRollbackSavepointAndWorkerRetry(t *testing.T) {
	a := newAuthority(t)
	var calls atomic.Int32
	c := Custom("custom", textSchema[InboxData](), func(_ context.Context, _ Member, _ DeliveryContext, p Input) (InboxData, error) {
		return InboxData{p.Text}, nil
	}, transportFunc[InboxData](func(ctx context.Context, _ DeliveryID, _ InboxData) (Outcome, error) {
		if attribution.FromContext(ctx).System() != "capture.original" {
			t.Error("delivery provenance changed")
		}
		if calls.Add(1) == 1 {
			return Retry, nil
		}
		return Accepted, nil
	}))
	b := Bind(Define("notice", 1, textSchema[Input]()), a.recipient, databaseChannel().Channel(), c)
	m, writer := fixture(t, b.Registration())
	policy := jobs.DefaultPolicy("notifications")
	policy.Backoff = []time.Duration{time.Millisecond}
	policy.Jitter = 0
	job := DefineDeliveryJob("notifications.deliver", policy)
	declaration, err := job.Declare(m)
	if err != nil {
		t.Fatal(err)
	}
	r, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := memory.New(memory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	queue := &durableQueue{backend}
	namespace := keyspace.Namespace{Application: "notifications", Environment: "test"}
	dispatcher, err := jobs.NewDispatcher(queue, r, jobs.DefaultDispatchConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	producer, err := jobs.PrepareOutbox("notifications", dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	route, err := producer.PublicationRoute()
	if err != nil {
		t.Fatal(err)
	}
	publication, err := publisher.New(writer, publisher.DefaultConfig(), route)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := (attribution.Origin{}).WithSystem("capture.original")
	if err != nil {
		t.Fatal(err)
	}
	captureContext, err := attribution.WithContext(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := b.Capture(captureContext, Member{ID: 1}.FoundryReference(), Input{"private snapshot"}, ID[Member]{})
	if err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("rollback")
	if err := writer.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := pending.Enqueue(t.Context(), tx, m, job, producer); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if result, err := publication.PublishOne(t.Context()); err != nil || result.Found {
		t.Fatal("rolled-back notification escaped", err)
	}
	// A failed outbox append must roll back notification rows even if the outer
	// business callback ignores the returned error and commits successfully.
	if err := writer.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := pending.Enqueue(t.Context(), tx, m, job, nil); err == nil {
			t.Error("nil outbox accepted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Transaction(t.Context(), func(tx *database.Tx) error {
		count, err := store.QueryFoundryNotifications().Count(t.Context(), tx)
		if count != 0 {
			t.Error("partial notification persisted")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Transaction(t.Context(), func(tx *database.Tx) error {
		var before, after string
		if err := database.ScanOne(t.Context(), tx, `SELECT current_setting('search_path')`, nil, &before); err != nil {
			return err
		}
		if _, err := pending.Enqueue(t.Context(), tx, m, job, producer); err != nil {
			return err
		}
		if err := database.ScanOne(t.Context(), tx, `SELECT current_setting('search_path')`, nil, &after); err != nil {
			return err
		}
		if before != after {
			t.Error("business transaction search path changed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if result, err := publication.PublishOne(t.Context()); err != nil || !result.Found || result.Failure != nil {
		t.Fatal("publication failed", err)
	}

	// A retry through a different request must reuse stored provenance as well as
	// the notification/job IDs, including when recaptured with the explicit ID.
	laterOrigin, err := (attribution.Origin{}).WithSystem("request.later")
	if err != nil {
		t.Fatal(err)
	}
	later, err := attribution.WithContext(t.Context(), laterOrigin)
	if err != nil {
		t.Fatal(err)
	}
	recaptured, err := b.Capture(later, Member{ID: 1}.FoundryReference(), Input{"private snapshot"}, pending.ID())
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Transaction(later, func(tx *database.Tx) error { _, err := recaptured.Enqueue(later, tx, m, job, producer); return err }); err != nil {
		t.Fatal(err)
	}
	if result, err := publication.PublishOne(t.Context()); err != nil || !result.Found || result.Failure != nil {
		t.Fatal("duplicate publication changed provenance", err)
	}
	jobID := model.IDFromBytes[jobs.ExecutionOf[DeliveryRequest]](pending.ID().Bytes())
	optional, err := job.definition.Inspect(t.Context(), dispatcher, jobID, "")
	if err != nil {
		t.Fatal(err)
	}
	record, ok := optional.Get()
	if !ok {
		t.Fatal("serialized delivery job missing")
	}
	if record.Envelope.Origin().System() != "capture.original" {
		t.Fatal("queue provenance changed")
	}
	encoded, err := record.Envelope.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.DecodeEnvelope(encoded); err != nil {
		t.Fatal("queue roundtrip", err)
	}
	config := jobs.DefaultWorkerConfig(namespace, "notifications")
	config.Concurrency = 1
	config.PollInterval = time.Millisecond
	worker, err := jobs.NewWorker(queue, r, config)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- worker.Run(context.Background()) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := worker.Stop(ctx); err != nil {
			t.Error(err)
			return
		}
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		optional, err := job.definition.Inspect(t.Context(), dispatcher, jobID, "")
		if err != nil {
			t.Fatal(err)
		}
		if record, ok := optional.Get(); ok && record.State == jobs.Succeeded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("notification job did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() != 2 {
		t.Fatal("worker did not retry exactly the failed channel")
	}
	inbox, err := a.recipient.Inbox(m)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := inbox.UnreadCount(a.context(t, 1)); err != nil || count != 1 {
		t.Fatal("worker duplicated database delivery", count, err)
	}
}
