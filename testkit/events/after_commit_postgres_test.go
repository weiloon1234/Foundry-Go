package events_test

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/events"
	eventstest "github.com/weiloon1234/Foundry-Go/testkit/events"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestPostgresRecorderObservesOnlyCommittedEvents(t *testing.T) {
	db := pgtest.Open(t)
	recorder, _ := eventstest.New[Notice](4)
	topic := events.Define[Notice]("test.committed", 1)
	declaration, err := topic.Declare(recorder.Listener("record"))
	if err != nil {
		t.Fatal(err)
	}
	bus := eventstest.Start(t, declaration)
	rollback := errors.New("rollback own transaction")
	err = db.Transaction(t.Context(), func(tx *database.Tx) error {
		if err := topic.AfterCommit(t.Context(), tx, bus, Notice{Values: map[string]string{"key": "committed"}}); err != nil {
			return err
		}
		if err := tx.Transaction(t.Context(), func(child *database.Tx) error {
			if err := topic.AfterCommit(t.Context(), child, bus, Notice{}); err != nil {
				return err
			}
			return rollback
		}); !errors.Is(err, rollback) {
			return errors.New("child transaction did not roll back")
		}
		if recorder.Count() != 0 {
			return errors.New("recorder dispatched before commit")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	eventstest.AssertCount(t, recorder, 1)
	err = db.Transaction(t.Context(), func(tx *database.Tx) error {
		if err := topic.AfterCommit(t.Context(), tx, bus, Notice{}); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	eventstest.AssertCount(t, recorder, 1)
}
