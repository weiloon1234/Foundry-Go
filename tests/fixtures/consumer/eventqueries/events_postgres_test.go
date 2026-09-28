package eventqueries_test

import (
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
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestModelObserverDispatchesTypedEventsOnlyAfterActualCommit(t *testing.T) {
	app := testkit.Start(t, foundry.New().Register(eventqueries.Domain(),
		events.Module("events", eventqueries.Bus, events.DefaultConfig()),
		postgres.Module("database", eventqueries.Pool, pgtest.Config(t))))
	db, err := foundation.Resolve(app.Services(), eventqueries.Pool)
	if err != nil {
		t.Fatal(err)
	}
	bus, err := foundation.Resolve(app.Services(), eventqueries.Bus)
	if err != nil {
		t.Fatal(err)
	}
	log, err := foundation.Resolve(app.Services(), eventqueries.Journal)
	if err != nil {
		t.Fatal(err)
	}
	namespace := pgtest.Namespace(t, db)
	path := `SET LOCAL search_path TO "` + namespace + `"`
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), path); err != nil {
			return err
		}
		_, err := tx.Exec(t.Context(), `CREATE TABLE observed_plain(id uuid PRIMARY KEY,name text NOT NULL)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	origin, err := (attribution.Origin{}).WithSystem("consumer.test")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := attribution.WithContext(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	var saved observerqueries.Plain
	if err := db.Transaction(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, path); err != nil {
			return err
		}
		var err error
		saved, err = observerqueries.QueryObservedPlain().Create(ctx, tx, observerqueries.PlainDraft{}.SetName("committed"))
		if err != nil {
			return err
		}
		if len(log.Entries()) != 0 {
			return errors.New("model event ran before outer commit")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	entries := log.Entries()
	if len(entries) != 1 || entries[0].Event.ID != saved.ID || entries[0].Origin != origin {
		t.Fatal("model event lost concrete key or origin")
	}
	rollback := errors.New("rollback model write")
	if err := db.Transaction(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, path); err != nil {
			return err
		}
		if _, err := observerqueries.QueryObservedPlain().Create(ctx, tx, observerqueries.PlainDraft{}.SetName("rolled back")); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) || len(log.Entries()) != 1 {
		t.Fatal("rolled back model dispatched an event", err)
	}
	rejected := errors.New("listener failed after commit")
	log.Reject(rejected)
	var retained model.ID[observerqueries.Plain]
	err = db.Transaction(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, path); err != nil {
			return err
		}
		item, err := observerqueries.QueryObservedPlain().Create(ctx, tx, observerqueries.PlainDraft{}.SetName("retained"))
		retained = item.ID
		return err
	})
	var failure *database.Error
	if !errors.Is(err, database.AfterCommitFailed) || !errors.Is(err, rejected) || !errors.As(err, &failure) || failure.Outcome() != database.Committed {
		t.Fatal("listener failure hid committed state", err)
	}
	log.Reject(nil)
	if err := db.Transaction(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, path); err != nil {
			return err
		}
		record, err := observerqueries.QueryObservedPlain().RequireFind(ctx, tx, retained)
		if err != nil {
			return err
		}
		if record.Name != "retained" {
			return errors.New("committed record missing")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Normal direct dispatch uses the same descriptor and concrete payload type.
	if err := eventqueries.Created.Dispatch(ctx, bus, eventqueries.RecordCreated{ID: saved.ID}); err != nil {
		t.Fatal(err)
	}
}
