package events_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/outbox"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func outboxTransactions(t *testing.T) func(context.Context, func(*database.Tx) error) error {
	t.Helper()
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	within := func(ctx context.Context, fn func(*database.Tx) error) error {
		return db.Transaction(ctx, func(tx *database.Tx) error {
			if _, err := tx.Exec(ctx, `SET LOCAL search_path TO "`+namespace+`"`); err != nil {
				return err
			}
			return fn(tx)
		})
	}
	definitions := outbox.Migrations()
	if _, err := migrate.New(definitions...); err != nil {
		t.Fatal(err)
	}
	// The production migration definitions run unchanged inside this fixture's
	// unique retained schema. Migration-runner history/locking has separate tests.
	if err := within(t.Context(), func(tx *database.Tx) error {
		for _, definition := range definitions {
			for _, statement := range definition.SQL {
				if _, err := tx.Exec(t.Context(), statement); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return within
}

func TestOutboxPersistsCapturedTypedPayloadWithoutDispatching(t *testing.T) {
	within := outboxTransactions(t)
	topic := events.Define[notice]("test.durable", 1)
	calls := 0
	bus := startedBus(t, events.DefaultConfig(), declaration(t, topic,
		events.Listen("record", func(context.Context, notice) error { calls++; return nil })))
	producer, err := events.PrepareOutbox("application.events", bus)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := (attribution.Origin{}).WithSystem("test.publisher")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := attribution.WithContext(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	input := notice{ID: 7, Values: map[string]string{"name": "captured"}}
	var id outbox.ID[notice]
	if err := within(ctx, func(tx *database.Tx) error {
		var err error
		id, err = topic.Enqueue(ctx, tx, producer, input)
		input.Values["name"] = "changed after enqueue"
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if id.IsZero() || calls != 0 {
		t.Fatal("enqueue omitted its identity or invoked an in-process listener")
	}
	if err := within(t.Context(), func(tx *database.Tx) error {
		result, err := topic.Find(t.Context(), tx, producer, id)
		if err != nil {
			return err
		}
		stored, present := result.Get()
		if !present || stored.ID() != id || stored.Origin() != origin || stored.CreatedAt().IsZero() {
			return errors.New("committed outbox record lost identity, origin or time")
		}
		first, err := stored.Payload()
		if err != nil {
			return err
		}
		if first.ID != 7 || first.Values["name"] != "captured" {
			return errors.New("outbox payload did not capture the original DTO")
		}
		first.Values["name"] = "changed decoded copy"
		second, err := stored.Payload()
		if err != nil {
			return err
		}
		if second.Values["name"] != "captured" {
			return errors.New("stored event retained a caller-owned payload map")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("typed reload dispatched a listener")
	}
}

func TestOutboxRowsFollowSavepointAndOuterRollback(t *testing.T) {
	within := outboxTransactions(t)
	topic := events.Define[int]("test.rollback", 1)
	bus := startedBus(t, events.DefaultConfig(), declaration(t, topic))
	producer, err := events.PrepareOutbox("application.events", bus)
	if err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("rollback outbox scope")
	var discarded, retained, outerRolledBack outbox.ID[int]
	if err := within(t.Context(), func(tx *database.Tx) error {
		if err := tx.Transaction(t.Context(), func(child *database.Tx) error {
			var err error
			discarded, err = topic.Enqueue(t.Context(), child, producer, 1)
			if err != nil {
				return err
			}
			return rollback
		}); !errors.Is(err, rollback) {
			return errors.New("child savepoint did not roll back")
		}
		return tx.Transaction(t.Context(), func(child *database.Tx) error {
			var err error
			retained, err = topic.Enqueue(t.Context(), child, producer, 2)
			return err
		})
	}); err != nil {
		t.Fatal(err)
	}
	if err := within(t.Context(), func(tx *database.Tx) error {
		var err error
		outerRolledBack, err = topic.Enqueue(t.Context(), tx, producer, 3)
		if err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal("outer transaction did not roll back", err)
	}
	if err := within(t.Context(), func(tx *database.Tx) error {
		for _, id := range []outbox.ID[int]{discarded, outerRolledBack} {
			result, err := topic.Find(t.Context(), tx, producer, id)
			if err != nil {
				return err
			}
			if result.IsSet() {
				return errors.New("rolled-back outbox record survived")
			}
		}
		result, err := topic.Find(t.Context(), tx, producer, retained)
		if err != nil {
			return err
		}
		if !result.IsSet() {
			return errors.New("successful savepoint lost its outbox record")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestOutboxReloadChecksDestinationVersionAndStoredPayloadSchema(t *testing.T) {
	within := outboxTransactions(t)
	topic := events.Define[notice]("test.schema", 1)
	newVersion := events.Define[notice]("test.schema", 2)
	bus := startedBus(t, events.DefaultConfig(), declaration(t, topic), declaration(t, newVersion))
	producer, err := events.PrepareOutbox("application.events", bus)
	if err != nil {
		t.Fatal(err)
	}
	other, err := events.PrepareOutbox("another.destination", bus)
	if err != nil {
		t.Fatal(err)
	}
	var id outbox.ID[notice]
	if err := within(t.Context(), func(tx *database.Tx) error {
		var err error
		id, err = topic.Enqueue(t.Context(), tx, producer, notice{ID: 9})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := within(t.Context(), func(tx *database.Tx) error {
		foreignDestination, err := topic.Find(t.Context(), tx, other, id)
		if err != nil {
			return err
		}
		foreignVersion, err := newVersion.Find(t.Context(), tx, producer, id)
		if err != nil {
			return err
		}
		if foreignDestination.IsSet() || foreignVersion.IsSet() {
			return errors.New("message ID bypassed its registered routing address")
		}
		// Deliberately corrupt this fixture's data through an explicit raw boundary.
		// Normal enqueue cannot create a DTO missing its required id property.
		_, err = tx.Exec(t.Context(), `UPDATE foundry_outbox SET payload='{}'::jsonb WHERE id=$1`, id.String())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := within(t.Context(), func(tx *database.Tx) error {
		_, err := topic.Find(t.Context(), tx, producer, id)
		if !errors.Is(err, fault.Invalid) {
			return errors.New("persisted payload bypassed its concrete event schema")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
