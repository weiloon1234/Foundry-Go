package orm_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func TestORMObserversAreApplicationOwnedWithoutLocalFactories(t *testing.T) {
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
	pool := foundation.NewKey[*database.DB]("lifecycle.pool")
	dependency := foundation.NewKey[string]("lifecycle.dependency")
	constructed, invoked, observed := 0, 0, 0

	modules := []foundation.Provider{
		database.Module("lifecycle.pool", pool, func() (database.Adapter, error) { return database.Adapter{Connector: observerConnector{}}, nil }, database.DefaultPoolConfig()),
		foundation.Module{Name: "lifecycle.observer", OnRegister: func(r *foundation.Registrar) error {
			if err := foundation.Provide(r, dependency, "injected"); err != nil {
				return err
			}
			return orm.RegisterObserver(r, pool, orm.NewObserver[record]("records.audit"), func(resolver foundation.Resolver) (func() orm.Hooks[record], error) {
				constructed++
				label, err := foundation.Resolve(resolver, dependency)
				if err != nil {
					return nil, err
				}
				return func() orm.Hooks[record] {
					invoked++
					return orm.Hooks[record]{Created: func(ctx context.Context, tx *database.Tx, c orm.Changes[record]) error {
						if label != "injected" {
							t.Error("dependency missing")
						}
						observed++
						return nil
					}}
				}, nil
			})
		}},
	}
	var previous *database.DB
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
		db, err := foundation.Resolve(app.Services(), pool)
		if err != nil {
			t.Fatal(err)
		}
		if db == previous || constructed != i || invoked != i-1 || observed != i-1 {
			t.Fatal("application observer ownership")
		}
		if err = app.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		_, err = table.Insert(t.Context(), db, table.Key().Set(1), table.Name.Set("input"))
		if err != nil || invoked != i || observed != i {
			t.Fatal("observer-only CRUD", err)
		}
		previous = db
	}
}

// The fake owns no external service; the configured application still constructs,
// freezes, starts and closes the real database/observer owners.
type observerConnector struct{}

func (observerConnector) Connect(context.Context) (driver.Conn, error) {
	return observerConnection{}, nil
}
func (observerConnector) Driver() driver.Driver            { return observerConnector{} }
func (observerConnector) Open(string) (driver.Conn, error) { return observerConnection{}, nil }

type observerConnection struct{}

func (observerConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (observerConnection) Close() error              { return nil }
func (observerConnection) Begin() (driver.Tx, error) { return observerTransaction{}, nil }
func (observerConnection) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return observerTransaction{}, nil
}
func (observerConnection) Ping(context.Context) error { return nil }
func (observerConnection) QueryContext(_ context.Context, _ string, args []driver.NamedValue) (driver.Rows, error) {
	return &observerRows{values: []driver.Value{args[0].Value, args[1].Value}}, nil
}

type observerTransaction struct{}

func (observerTransaction) Commit() error   { return nil }
func (observerTransaction) Rollback() error { return nil }

type observerRows struct {
	values []driver.Value
	done   bool
}

func (*observerRows) Columns() []string { return []string{"id", "name"} }
func (*observerRows) Close() error      { return nil }
func (r *observerRows) Next(dst []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	copy(dst, r.values)
	return nil
}
