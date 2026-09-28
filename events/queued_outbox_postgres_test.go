package events_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/internal/outboxtest"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

type eventQueuePeer struct{ *memory.Backend }

func (eventQueuePeer) DurableAcceptance() bool { return true }

type queuedNotice struct {
	Text string `json:"text"`
}

func TestOutboxPublicationQueuesTypedEventAndPreservesDeliveryIdentity(t *testing.T) {
	writer := outboxtest.Open(t)
	topic := events.Define[queuedNotice]("queued.notice", 1)
	var calls atomic.Int32
	var expected outbox.ID[queuedNotice]
	declaration, err := topic.Declare(events.Listen("notice", func(ctx context.Context, input queuedNotice) error {
		id, ok := topic.OutboxID(ctx)
		if !ok || id != expected || input.Text != "original" {
			return errors.New("event identity or payload lost")
		}
		if calls.Add(1) == 1 {
			return errors.New("transient listener failure")
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	bus, err := events.Prepare(events.DefaultConfig(), declaration)
	if err != nil {
		t.Fatal(err)
	}
	if err := bus.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer bus.Close(context.Background())
	producer, err := events.PrepareOutbox("events", bus)
	if err != nil {
		t.Fatal(err)
	}
	policy := jobs.DefaultPolicy("events")
	policy.Backoff = []time.Duration{0}
	policy.Jitter = 0
	delivery, err := events.NewQueuedOutbox(policy, producer)
	if err != nil {
		t.Fatal(err)
	}
	jobDeclaration, err := delivery.Declaration()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(jobDeclaration)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := memory.New(memory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	peer := eventQueuePeer{backend}
	namespace := keyspace.Namespace{Application: "events", Environment: "test"}
	dispatcher, err := jobs.NewDispatcher(peer, registry, jobs.DefaultDispatchConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	route, err := delivery.PublicationRoute(producer.Destination(), dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	config := publisher.DefaultConfig()
	config.Clock = testkit.NewClock(time.Now().Add(time.Second))
	p, err := publisher.New(writer, config, route)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Transaction(t.Context(), func(tx *database.Tx) error {
		var err error
		expected, err = topic.Enqueue(t.Context(), tx, producer, queuedNotice{Text: "original"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	result, err := p.PublishOne(t.Context())
	if err != nil || result.State != outbox.Published || !result.Committed || calls.Load() != 0 {
		t.Fatal("publication executed listener or lost acceptance", err)
	}
	workerConfig := jobs.DefaultWorkerConfig(namespace, "events")
	workerConfig.PollInterval = time.Millisecond
	worker, err := jobs.NewWorker(peer, registry, workerConfig)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- worker.Run(t.Context()) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := worker.Stop(ctx); err != nil {
			t.Error(err)
			return
		}
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		page, err := dispatcher.List(t.Context(), "events", jobs.ListOptions{State: jobs.Succeeded, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Records) == 1 {
			if calls.Load() != 2 || page.Records[0].Attempts != 2 {
				t.Fatal("event retries changed execution")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("queued event never completed")
}
