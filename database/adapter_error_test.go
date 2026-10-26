package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// Native hook callbacks preserve failure identity and resource ownership. Native
// factories do not return errors; the retired adapter-construction API is absent.
func TestObserverErrorRollsBackBeforeSQL(t *testing.T) {
	state := mutationDriver([][]driver.Value{{int64(42), "record"}})
	db, err := database.Prepare(database.Adapter{Connector: connector{state}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	veto := errors.New("registered veto")
	calls := 0
	declaration, err := orm.NewObserver[mutationRecord]("observer").Declare(func() orm.Hooks[mutationRecord] {
		calls++
		return orm.Hooks[mutationRecord]{Creating: func(_ context.Context, tx *database.Tx, _ *orm.Write[mutationRecord]) error {
			if tx == nil {
				t.Error("observer lost owning transaction")
			}
			return veto
		}}
	})
	if err != nil {
		t.Fatal(err)
	}
	set, err := lifecycle.NewObservers(declaration)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.BindObservers(set); err != nil {
		t.Fatal(err)
	}
	if err := db.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, err = mutationFields.Insert(t.Context(), observerTransactor{db}, mutationValues()...)
	if !errors.Is(err, veto) || state.rolledBack.Load() != 1 || state.rowsClosed.Load() != 0 {
		t.Fatal("registered factory escaped transaction or executed SQL after failure", err)
	}
	if calls != 1 {
		t.Fatal("factory repeated or stale adapter called")
	}
}

func TestRetrievalErrorDiscardsResults(t *testing.T) {
	cause := errors.New("retrieval fixture failure")
	state := retrievalFixtureDriver([][]driver.Value{{int64(1), "stored"}})
	db := open(t, state, oneObserverConnection)
	factories := 0
	q := retrievalTable(orm.WithRetrievalHooks(func() orm.RetrievalHooks[retrievalFixtureRecord] {
		factories++
		return orm.RetrievalHooks[retrievalFixtureRecord]{Retrieved: func(context.Context, database.Executor, retrievalFixtureRecord) error { return cause }}
	})).Query()
	items, err := q.All(t.Context(), db)
	if !errors.Is(err, cause) || items != nil || factories != 1 {
		t.Fatal("failed retrieval published partial models", items, err)
	}
	if state.rowsClosed.Load() != 1 || db.Stats().Owners != 0 || db.Stats().InUse != 0 {
		t.Fatal("retrieval failure leaked rows or owners")
	}
}
