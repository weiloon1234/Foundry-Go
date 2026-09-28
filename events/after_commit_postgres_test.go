package events_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/events"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

type transactionContextKey struct{}

func TestAfterCommitCapturesOriginAndPayloadAndUsesOuterLifetime(t *testing.T) {
	db := pgtest.Open(t)
	topic := events.Define[notice]("test.committed", 1)
	var ids []int
	var values []string
	var origins []attribution.SystemID
	bus := startedBus(t, events.DefaultConfig(), declaration(t, topic, events.Listen("record", func(ctx context.Context, payload notice) error {
		if ctx.Err() != nil || ctx.Value(transactionContextKey{}) != "outer" {
			return errors.New("listener used the canceled work context")
		}
		ids = append(ids, payload.ID)
		values = append(values, payload.Values["value"])
		origins = append(origins, attribution.FromContext(ctx).System())
		return nil
	})))
	outer := context.WithValue(t.Context(), transactionContextKey{}, "outer")
	rollback := errors.New("rollback child")
	err := db.Transaction(outer, func(tx *database.Tx) error {
		origin, err := (attribution.Origin{}).WithSystem("original.origin")
		if err != nil {
			return err
		}
		attributed, err := attribution.WithContext(outer, origin)
		if err != nil {
			return err
		}
		work, cancel := context.WithCancel(context.WithValue(attributed, transactionContextKey{}, "work"))
		input := notice{ID: 1, Values: map[string]string{"value": "original"}}
		if err := topic.AfterCommit(work, tx, bus, input); err != nil {
			cancel()
			return err
		}
		input.Values["value"] = "changed"
		cancel()
		if err := tx.Transaction(outer, func(child *database.Tx) error {
			if err := topic.AfterCommit(attributed, child, bus, notice{ID: 2}); err != nil {
				return err
			}
			return rollback
		}); !errors.Is(err, rollback) {
			return errors.New("child rollback failed")
		}
		if err := tx.Transaction(outer, func(child *database.Tx) error { return topic.AfterCommit(attributed, child, bus, notice{ID: 3}) }); err != nil {
			return err
		}
		if len(ids) != 0 {
			return errors.New("event dispatched before the outer commit")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []int{1, 3}) || !reflect.DeepEqual(values, []string{"original", ""}) || !reflect.DeepEqual(origins, []attribution.SystemID{"original.origin", "original.origin"}) {
		t.Fatal("after-commit capture or savepoint ordering was lost", ids, values, origins)
	}
	ids = nil
	err = db.Transaction(outer, func(tx *database.Tx) error {
		if err := topic.AfterCommit(outer, tx, bus, notice{ID: 4}); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) || len(ids) != 0 {
		t.Fatal("outer rollback dispatched an event", err)
	}
}

func TestAfterCommitFailurePreservesCommittedOutcomeAndLaterCallbacks(t *testing.T) {
	db := pgtest.Open(t)
	topic := events.Define[int]("test.commit_failure", 1)
	rejected := errors.New("listener rejected")
	bus := startedBus(t, events.DefaultConfig(), declaration(t, topic, events.Listen("reject", func(context.Context, int) error { return rejected })))
	later := false
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if err := topic.AfterCommit(t.Context(), tx, bus, 7); err != nil {
			return err
		}
		return tx.AfterCommit(func(context.Context) error { later = true; return nil })
	})
	var failure *database.Error
	if !errors.Is(err, database.AfterCommitFailed) || !errors.Is(err, rejected) || !errors.As(err, &failure) || failure.Outcome() != database.Committed || !later {
		t.Fatal("event failure lost committed outcome or skipped later callbacks", err)
	}
}
