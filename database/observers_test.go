package database_test

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

type observedRecord struct{}
type registeredHooks struct {
	name       string
	dependency int
}

func observerNames(t *testing.T, set lifecycle.Observers) []string {
	t.Helper()
	factories, err := lifecycle.ObserverFactories[observedRecord, registeredHooks](set)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, factory := range factories {
		names = append(names, factory().name)
	}
	return names
}

func TestObserverModuleBindsBeforeBootWithDependencyOrderAndPoolIsolation(t *testing.T) {
	firstKey, secondKey := foundation.NewKey[*database.DB]("first.pool"), foundation.NewKey[*database.DB]("second.pool")
	dependency := foundation.NewKey[int]("observer.dependency")
	state := &driverState{}
	adapter := func() (database.Adapter, error) { return database.Adapter{Connector: connector{state}}, nil }
	firstPool := database.Module("first.pool", firstKey, adapter, database.DefaultPoolConfig())
	secondPool := database.Module("second.pool", secondKey, adapter, database.DefaultPoolConfig())
	var constructed, invoked atomic.Int64
	var retained *foundation.Registrar
	register := func(r *foundation.Registrar, pool foundation.Key[*database.DB], name string) error {
		retained = r
		key := lifecycle.NewObserver[observedRecord, registeredHooks](name)
		return database.RegisterObserver(r, pool, key, func(resolver foundation.Resolver) (func() registeredHooks, error) {
			constructed.Add(1)
			db, err := foundation.Resolve(resolver, pool)
			if err != nil {
				return nil, err
			}
			if err := db.Start(t.Context()); !errors.Is(err, database.NotReady) {
				t.Errorf("pool started during observer construction: %v", err)
			}
			if err := db.BindObservers(lifecycle.Observers{}); !errors.Is(err, fault.Invalid) {
				t.Errorf("module binding replaced: %v", err)
			}
			value, err := foundation.Resolve(resolver, dependency)
			if err != nil {
				return nil, err
			}
			return func() registeredHooks { invoked.Add(1); return registeredHooks{name: name, dependency: value} }, nil
		})
	}
	modules := []foundation.Provider{
		foundation.Module{Name: "second.observers", Requires: []foundation.ProviderID{"first.observers"}, OnRegister: func(r *foundation.Registrar) error {
			if err := register(r, firstKey, "second"); err != nil {
				return err
			}
			return register(r, secondKey, "first") // identifiers are scoped to their pool
		}},
		firstPool, secondPool,
		foundation.Module{Name: "first.observers", OnRegister: func(r *foundation.Registrar) error {
			if err := foundation.Provide(r, dependency, 42); err != nil {
				return err
			}
			return register(r, firstKey, "first")
		}},
	}
	var previous *database.DB
	for range 2 {
		beforeConstruct, beforeInvoke := constructed.Load(), invoked.Load()
		app, err := foundry.New().Register(modules...).Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		first, err := foundation.Resolve(app.Services(), firstKey)
		if err != nil {
			t.Fatal(err)
		}
		second, err := foundation.Resolve(app.Services(), secondKey)
		if err != nil {
			t.Fatal(err)
		}
		if first == previous || first == second || first.Stats().Ready || constructed.Load()-beforeConstruct != 3 || invoked.Load() != beforeInvoke {
			t.Fatal("observer construction started resources, ran hooks or shared application state")
		}
		if got := observerNames(t, first.Observers()); !reflect.DeepEqual(got, []string{"first", "second"}) {
			t.Fatal("provider dependency order lost", got)
		}
		if got := observerNames(t, second.Observers()); !reflect.DeepEqual(got, []string{"first"}) {
			t.Fatal("pool observers leaked", got)
		}
		factories, err := lifecycle.ObserverFactories[observedRecord, registeredHooks](first.Observers())
		if err != nil || factories[1]().dependency != 42 {
			t.Fatal("injected service missing", err)
		}
		if err := register(retained, firstKey, "late"); !errors.Is(err, fault.Closed) {
			t.Fatal("retained registrar changed frozen observers", err)
		}
		if err := app.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := first.BindObservers(lifecycle.Observers{}); !errors.Is(err, fault.Closed) {
			t.Fatal("started observer set changed", err)
		}
		if err := app.Shutdown(t.Context()); err != nil {
			t.Fatal(err)
		}
		previous = first
	}
	if state.connected.Load() != 4 || state.closed.Load() != 4 {
		t.Fatal("pool resource ownership changed")
	}
}

func TestObserverConstructionFailuresPreventPoolStart(t *testing.T) {
	key := foundation.NewKey[*database.DB]("observed.pool")
	missing := foundation.NewKey[int]("missing")
	cause := errors.New("observer dependency failed")
	for _, mode := range []string{"duplicate", "missing", "cycle", "error", "panic", "goexit", "nil", "unmanaged"} {
		t.Run(mode, func(t *testing.T) {
			state := &driverState{}
			pool := database.Module("pool", key, func() (database.Adapter, error) { return database.Adapter{Connector: connector{state}}, nil }, database.DefaultPoolConfig())
			var captured foundation.Resolver
			provider := foundation.Module{Name: "observers", OnRegister: func(r *foundation.Registrar) error {
				if mode == "cycle" {
					if err := foundation.Factory(r, missing, func(s foundation.Resolver) (int, error) { return foundation.Resolve(s, missing) }); err != nil {
						return err
					}
				}
				add := func() error {
					return database.RegisterObserver(r, key, lifecycle.NewObserver[observedRecord, registeredHooks]("audit"), func(s foundation.Resolver) (func() registeredHooks, error) {
						captured = s
						switch mode {
						case "missing", "cycle":
							_, err := foundation.Resolve(s, missing)
							return nil, err
						case "error":
							return nil, cause
						case "panic":
							panic(cause)
						case "goexit":
							runtime.Goexit()
						case "nil":
							return nil, nil
						}
						return func() registeredHooks { return registeredHooks{} }, nil
					})
				}
				if err := add(); err != nil {
					return err
				}
				if mode == "duplicate" {
					return add()
				}
				return nil
			}}
			if mode == "unmanaged" {
				pool = foundation.Module{Name: "pool", OnRegister: func(r *foundation.Registrar) error {
					db, err := database.Prepare(database.Adapter{Connector: connector{state}}, database.DefaultPoolConfig())
					if err != nil {
						return err
					}
					t.Cleanup(func() { _ = db.Close(context.Background()) })
					return foundation.Provide(r, key, db)
				}}
			}
			_, err := foundry.New().Register(pool, provider).Build(t.Context())
			want := map[string]error{"duplicate": fault.Duplicate, "missing": fault.Missing, "cycle": fault.Cycle, "error": cause, "panic": fault.Panicked, "goexit": fault.Panicked, "nil": fault.Invalid, "unmanaged": fault.Invalid}[mode]
			if !errors.Is(err, want) || state.connected.Load() != 0 {
				t.Fatal("construction failure did not stop boot", err)
			}
			if captured != nil {
				if _, err := foundation.Resolve(captured, key); !errors.Is(err, fault.Closed) {
					t.Fatal("constructor resolver escaped", err)
				}
			}
		})
	}
}

type observerTransactor struct{ inner database.Transactor }

func (w observerTransactor) Transaction(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
	return w.inner.Transaction(ctx, fn, options...)
}

func TestObserverOwnershipSurvivesSessionsTransactionsSavepointsAndWrappers(t *testing.T) {
	declaration, err := lifecycle.NewObserver[observedRecord, registeredHooks]("retained").Declare(func() registeredHooks { return registeredHooks{name: "retained"} })
	if err != nil {
		t.Fatal(err)
	}
	set, err := lifecycle.NewObservers(declaration)
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.Prepare(database.Adapter{Connector: connector{&driverState{}}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if err := db.BindObservers(set); err != nil {
		t.Fatal(err)
	}
	if err := db.BindObservers(set); !errors.Is(err, fault.Closed) {
		t.Fatal("binding repeated", err)
	}
	if err := db.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	check := func(set lifecycle.Observers) error {
		if !reflect.DeepEqual(observerNames(t, set), []string{"retained"}) {
			return errors.New("observer ownership lost")
		}
		return nil
	}
	rollback := errors.New("rollback child")
	write := func(tx *database.Tx) error {
		if err := check(tx.Observers()); err != nil {
			return err
		}
		if err := tx.Savepoint(t.Context(), func(child *database.Tx) error {
			if err := check(child.Observers()); err != nil {
				return err
			}
			return observerTransactor{child}.Transaction(t.Context(), func(nested *database.Tx) error {
				if err := check(nested.Observers()); err != nil {
					return err
				}
				return rollback
			})
		}); !errors.Is(err, rollback) {
			return errors.New("nested rollback changed")
		}
		return tx.Transaction(t.Context(), func(child *database.Tx) error { return check(child.Observers()) })
	}
	if err := (observerTransactor{db}).Transaction(t.Context(), write); err != nil {
		t.Fatal(err)
	}
	if err := db.Session(t.Context(), func(s *database.Session) error {
		if err := check(s.Observers()); err != nil {
			return err
		}
		return observerTransactor{s}.Transaction(t.Context(), write)
	}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := db.Transaction(t.Context(), func(tx *database.Tx) error { return check(tx.Observers()) }); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	other := open(t, &driverState{}, nil)
	if lifecycle.HasObservers[observedRecord](other.Observers()) {
		t.Fatal("independent pool inherited observers")
	}
	if err := other.BindObservers(set); !errors.Is(err, fault.Closed) {
		t.Fatal("running empty pool accepted binding", err)
	}
}
