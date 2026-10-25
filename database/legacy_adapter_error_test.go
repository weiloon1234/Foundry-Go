package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

// These two error-returning adapter boundaries have no native factory equivalent:
// ORM installs the adapter and its factories return hooks, not errors. Keep their
// assertions executable until the lead settles their disposition at 8d3.
func TestLegacyObserverAdapterErrorRollsBackBeforeSQL(t *testing.T) {
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
	declaration, err := lifecycle.NewObserver[mutationRecord, query.WriteHooks[mutationRecord]]("observer").Declare(func() query.WriteHooks[mutationRecord] { return query.WriteHooks[mutationRecord]{} })
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
	veto := errors.New("registered veto")
	calls := 0
	definition := query.Define("records", "id", []query.Column{{Name: "id", DatabaseDefault: true}, {Name: "name"}}, func(row database.Row) (mutationRecord, error) {
		var r mutationRecord
		err := row.Scan(codec.Signed[int64]().Scan(&r.ID), codec.String[string]().Scan(&r.Name))
		return r, err
	}, query.NewModelField("id", codec.Signed[int64](), func(r mutationRecord) int64 { return r.ID }), query.NewModelField("name", codec.String[string](), func(r mutationRecord) string { return r.Name }))
	definition = definition.WithObserverHooks(func(_ context.Context, owned lifecycle.Observers) (query.WriteHooks[mutationRecord], error) {
		calls++
		if !lifecycle.HasObservers[mutationRecord](owned) {
			t.Error("adapter lost owning transaction observers")
		}
		return query.WriteHooks[mutationRecord]{}, veto
	}, false)
	_, err = query.ForModel(definition).Insert(t.Context(), observerTransactor{db}, query.Change(query.Assign[mutationRecord]("records", "name", codec.String[string](), "private record name")))
	if !errors.Is(err, veto) || state.rolledBack.Load() != 1 || state.rowsClosed.Load() != 0 {
		t.Fatal("registered factory escaped transaction or executed SQL after failure", err)
	}
	if calls != 1 {
		t.Fatal("factory repeated or stale adapter called")
	}
}

// Also used by lane C's pending legacy record-iteration capture.
func retrievalFixtureDefinition() query.Definition[retrievalFixtureRecord] {
	return query.Define("retrieved_records", "id", []query.Column{{Name: "id"}, {Name: "name"}}, func(row database.Row) (retrievalFixtureRecord, error) {
		var item retrievalFixtureRecord
		err := row.Scan(&item.ID, &item.Name)
		return item, err
	}, query.NewModelField("id", codec.Signed[int](), func(item retrievalFixtureRecord) int { return item.ID }), query.NewModelField("name", codec.String[string](), func(item retrievalFixtureRecord) string { return item.Name }))
}

func TestLegacyRetrievalAdapterErrorDiscardsResults(t *testing.T) {
	cause := errors.New("retrieval fixture failure")
	state := retrievalFixtureDriver([][]driver.Value{{int64(1), "stored"}})
	db := open(t, state, oneObserverConnection)
	factories := 0
	q := query.ForModel(retrievalFixtureDefinition().WithRetrievalHooks(func(context.Context, lifecycle.Observers) (query.RetrievalHooks[retrievalFixtureRecord], error) {
		factories++
		return query.RetrievalHooks[retrievalFixtureRecord]{}, cause
	}, true))
	items, err := q.All(t.Context(), db)
	if !errors.Is(err, cause) || items != nil || factories != 1 {
		t.Fatal("failed retrieval published partial models", items, err)
	}
	if state.rowsClosed.Load() != 1 || db.Stats().Owners != 0 || db.Stats().InUse != 0 {
		t.Fatal("retrieval failure leaked rows or owners")
	}
}
