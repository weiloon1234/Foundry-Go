package orm_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func TestRetrievalObserversAreApplicationOwnedWithoutLocalFactory(t *testing.T) {
	type record struct {
		ID   int64
		Name string
	}
	type fields struct {
		orm.Table[record, int64]
		Name orm.Text[record, string]
	}
	table := orm.Define("observer_records", func(b orm.Builder[record], m *record) fields {
		return fields{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name)}
	})
	pool := foundation.NewKey[*database.DB]("retrieval.pool")
	constructed, invoked, seen := 0, 0, 0
	modules := []foundation.Provider{
		database.Module("retrieval.pool", pool, func() (database.Adapter, error) { return database.Adapter{Connector: observerConnector{}}, nil }, database.DefaultPoolConfig()),
		foundation.Module{Name: "retrieval.observer", OnRegister: func(r *foundation.Registrar) error {
			return orm.RegisterRetrievalObserver(r, pool, orm.NewRetrievalObserver[record]("records.read"), func(foundation.Resolver) (func() orm.RetrievalHooks[record], error) {
				constructed++
				return func() orm.RetrievalHooks[record] {
					invoked++
					return orm.RetrievalHooks[record]{Retrieved: func(context.Context, database.Executor, record) error { seen++; return nil }}
				}, nil
			})
		}},
	}
	for i := 1; i <= 2; i++ {
		settings := application.DefaultSettings()
		settings.HTTP.Enabled = false
		app, err := application.New(settings, application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))).Register(modules...).Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := app.Shutdown(context.Background()); err != nil {
				t.Error(err)
			}
		})
		if constructed != i || invoked != i-1 {
			t.Fatal("assembly invoked read factory")
		}
		if err = app.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		db, err := foundation.Resolve(app.Services(), pool)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := table.Query().Where(table.Key().Eq(1), table.Name.Eq("name")).All(t.Context(), db)
		if err != nil || len(rows) != 1 || invoked != i || seen != i {
			t.Fatal("retrieval-only application dispatch", err, invoked, seen)
		}
	}
}
