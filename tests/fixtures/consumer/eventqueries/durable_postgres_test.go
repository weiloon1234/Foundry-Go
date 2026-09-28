package eventqueries_test

import (
	"context"
	"errors"
	"testing"

	"foundry.test/consumer/eventqueries"
	"foundry.test/consumer/observerqueries"
	"github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

type durableConsumer struct {
	producer *events.Outbox
	trace    *eventqueries.EnqueueTrace
	within   func(context.Context, func(*database.Tx) error) error
}

func prepareDurableConsumer(t *testing.T, migrateOutbox bool) durableConsumer {
	t.Helper()
	app := testkit.Start(t, foundry.New().Register(eventqueries.DurableDomain(),
		events.Module("events", eventqueries.Bus, events.DefaultConfig()),
		postgres.Module("database", eventqueries.Pool, pgtest.Config(t))))
	db, err := foundation.Resolve(app.Services(), eventqueries.Pool)
	if err != nil {
		t.Fatal(err)
	}
	producer, err := foundation.Resolve(app.Services(), eventqueries.Durable)
	if err != nil {
		t.Fatal(err)
	}
	trace, err := foundation.Resolve(app.Services(), eventqueries.Enqueued)
	if err != nil {
		t.Fatal(err)
	}
	namespace := pgtest.Namespace(t, db)
	within := func(ctx context.Context, fn func(*database.Tx) error) error {
		return db.Transaction(ctx, func(tx *database.Tx) error {
			if _, err := tx.Exec(ctx, `SET LOCAL search_path TO "`+namespace+`"`); err != nil {
				return err
			}
			return fn(tx)
		})
	}
	if err := within(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `CREATE TABLE observed_plain(id uuid PRIMARY KEY,name text NOT NULL)`); err != nil {
			return err
		}
		if migrateOutbox {
			// Apply the unchanged framework definitions in this retained test schema.
			for _, definition := range outbox.Migrations() {
				for _, statement := range definition.SQL {
					if _, err := tx.Exec(t.Context(), statement); err != nil {
						return err
					}
				}
			}
			if _, err := tx.Exec(t.Context(), `CREATE TABLE event_links(id bigint PRIMARY KEY,message_id uuid NOT NULL REFERENCES foundry_outbox(id))`); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return durableConsumer{producer: producer, trace: trace, within: within}
}

func TestModelAndTypedOutboxCommitAndRollBackTogether(t *testing.T) {
	consumer := prepareDurableConsumer(t, true)
	origin, err := (attribution.Origin{}).WithSystem("consumer.writer")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := attribution.WithContext(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	var saved observerqueries.Plain
	if err := consumer.within(ctx, func(tx *database.Tx) error {
		var err error
		saved, err = observerqueries.QueryObservedPlain().Create(ctx, tx, observerqueries.PlainDraft{}.SetName("committed"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	ids := consumer.trace.IDs()
	if len(ids) != 1 {
		t.Fatal("model observer did not enqueue exactly one message")
	}
	if err := consumer.within(t.Context(), func(tx *database.Tx) error {
		result, err := eventqueries.Created.Find(t.Context(), tx, consumer.producer, ids[0])
		if err != nil {
			return err
		}
		stored, present := result.Get()
		if !present || stored.Origin() != origin {
			return errors.New("committed event lost its original provenance")
		}
		payload, err := stored.Payload()
		if err != nil {
			return err
		}
		if payload.ID != saved.ID {
			return errors.New("event lost the concrete persisted model key")
		}
		if _, err := eventqueries.QueryEventLinks().Create(t.Context(), tx, eventqueries.EventLinkDraft{}.SetID(1).SetMessageID(stored.ID())); err != nil {
			return err
		}
		link, err := eventqueries.QueryEventLinks().Where(eventqueries.EventLinkFields().MessageID.Eq(ids[0])).RequireFirst(t.Context(), tx)
		if err != nil {
			return err
		}
		if link.MessageID != ids[0] {
			return errors.New("generated model changed the payload-owned message key")
		}
		_, err = observerqueries.QueryObservedPlain().RequireFind(t.Context(), tx, payload.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("rollback business transaction")
	var discarded model.ID[observerqueries.Plain]
	if err := consumer.within(ctx, func(tx *database.Tx) error {
		item, err := observerqueries.QueryObservedPlain().Create(ctx, tx, observerqueries.PlainDraft{}.SetName("discarded"))
		discarded = item.ID
		if err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal("outer rollback was lost", err)
	}
	ids = consumer.trace.IDs()
	if len(ids) != 2 {
		t.Fatal("rollback fixture did not reach its model observer")
	}
	if err := consumer.within(t.Context(), func(tx *database.Tx) error {
		message, err := eventqueries.Created.Find(t.Context(), tx, consumer.producer, ids[1])
		if err != nil {
			return err
		}
		row, err := observerqueries.QueryObservedPlain().Find(t.Context(), tx, discarded)
		if err != nil {
			return err
		}
		if message.IsSet() || row.IsSet() {
			return errors.New("outbox and business model did not roll back together")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFailureAfterEnqueueRollsBackTheModelAndMessage(t *testing.T) {
	consumer := prepareDurableConsumer(t, true)
	rejected := errors.New("later observer failure")
	consumer.trace.Reject(rejected)
	if err := consumer.within(t.Context(), func(tx *database.Tx) error {
		_, err := observerqueries.QueryObservedPlain().Create(t.Context(), tx, observerqueries.PlainDraft{}.SetName("rejected"))
		return err
	}); !errors.Is(err, rejected) {
		t.Fatal("observer failure was lost", err)
	}
	ids := consumer.trace.IDs()
	if len(ids) != 1 {
		t.Fatal("fixture did not reach enqueue before the injected failure")
	}
	if err := consumer.within(t.Context(), func(tx *database.Tx) error {
		message, err := eventqueries.Created.Find(t.Context(), tx, consumer.producer, ids[0])
		if err != nil {
			return err
		}
		rows, err := observerqueries.QueryObservedPlain().Count(t.Context(), tx)
		if err != nil {
			return err
		}
		if message.IsSet() || rows != 0 {
			return errors.New("later observer failure retained a model or queued message")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestOutboxStorageFailureRollsBackTheModelWrite(t *testing.T) {
	consumer := prepareDurableConsumer(t, false)
	if err := consumer.within(t.Context(), func(tx *database.Tx) error {
		_, err := observerqueries.QueryObservedPlain().Create(t.Context(), tx, observerqueries.PlainDraft{}.SetName("missing outbox"))
		return err
	}); err == nil {
		t.Fatal("missing outbox table did not reject the model write")
	}
	if len(consumer.trace.IDs()) != 0 {
		t.Fatal("failed enqueue returned a successful message identity")
	}
	if err := consumer.within(t.Context(), func(tx *database.Tx) error {
		count, err := observerqueries.QueryObservedPlain().Count(t.Context(), tx)
		if err != nil {
			return err
		}
		if count != 0 {
			return errors.New("outbox failure retained its business model")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
