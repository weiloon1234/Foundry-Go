package database_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func TestPreparedPoolIsPureAndCannotExecuteBeforeStart(t *testing.T) {
	state := &driverState{}
	db, err := database.Prepare(database.Adapter{Connector: connector{state}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(t.Context())
	if state.connected.Load() != 0 || db.Stats().Ready {
		t.Fatal("prepared pool acquired resources")
	}
	if _, err := db.Exec(t.Context(), "not yet started"); !errors.Is(err, database.NotReady) {
		t.Fatal("prepared pool executed")
	}
	if err := db.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !db.Stats().Ready || state.connected.Load() != 1 {
		t.Fatal("pool did not start")
	}
	if err := db.Start(t.Context()); err != nil || state.connected.Load() != 1 {
		t.Fatal("start was not idempotent")
	}
}

func TestPoolConcurrentStartWaitersAndCloseDuringStartup(t *testing.T) {
	entered, unblock := make(chan struct{}), make(chan struct{})
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(unblock) }) })
	state := &driverState{ping: func(ctx context.Context) error { close(entered); <-unblock; return ctx.Err() }}
	db, err := database.Prepare(database.Adapter{Connector: connector{state}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan error, 1)
	go func() { started <- db.Start(t.Context()) }()
	<-entered
	waiter, cancel := context.WithCancel(t.Context())
	cancel()
	if err := db.Start(waiter); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled waiter did not return")
	}
	if state.connected.Load() != 1 || db.Stats().Owners != 1 {
		t.Fatal("waiter altered startup ownership")
	}
	closing, stop := context.WithTimeout(t.Context(), 5*time.Millisecond)
	defer stop()
	if err := db.Close(closing); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("close passed active startup")
	}
	select {
	case <-db.Done():
		t.Fatal("startup resources closed prematurely")
	default:
	}
	release.Do(func() { close(unblock) })
	if err := <-started; !errors.Is(err, database.Closed) {
		t.Fatalf("startup became ready after close: %v", err)
	}
	if err := db.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if state.closed.Load() != 1 || db.Stats().Ready {
		t.Fatal("startup close leaked")
	}
}

func TestFailedStartIsTerminalAndPreparedCloseNeedsNoConnection(t *testing.T) {
	cause := errors.New("connectivity failure")
	state := &driverState{ping: func(context.Context) error { return cause }}
	db, err := database.Prepare(database.Adapter{Connector: connector{state}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := db.Start(t.Context()); !errors.Is(err, cause) {
			t.Fatal("startup failure lost")
		}
	}
	if state.connected.Load() != 1 || state.closed.Load() != 1 || db.Stats().Ready {
		t.Fatal("failed start was retried or leaked")
	}
	if err := db.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	unused := &driverState{}
	db, err = database.Prepare(database.Adapter{Connector: connector{unused}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := db.Start(t.Context()); !errors.Is(err, database.Closed) || unused.connected.Load() != 0 {
		t.Fatal("closed prepared pool connected")
	}
}

func TestDatabaseModuleConstructsIndependentPoolsDuringBuild(t *testing.T) {
	state := &driverState{}
	key := foundation.NewKey[*database.DB]("test.database")
	module := database.Module("test.database", key, func() (database.Adapter, error) { return database.Adapter{Connector: connector{state}}, nil }, database.DefaultPoolConfig())
	var previous *database.DB
	for range 2 {
		app, err := foundry.New().Register(module).Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		db, err := foundation.Resolve(app.Services(), key)
		if err != nil {
			t.Fatal(err)
		}
		if db == previous || db.Stats().Ready {
			t.Fatal("application construction shared or started pool")
		}
		if err := app.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := db.Ping(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := app.Shutdown(t.Context()); err != nil {
			t.Fatal(err)
		}
		select {
		case <-db.Done():
		default:
			t.Fatal("application stopped before pool")
		}
		previous = db
	}
	// Each application opens one pool connection at Start and one dedicated
	// readiness probe connection for Ping; shutdown closes both.
	if state.connected.Load() != 4 || state.closed.Load() != 4 {
		t.Fatal("application pool ownership mismatch")
	}
}

func TestDatabaseModuleCleansUpAfterLaterBootFailure(t *testing.T) {
	state := &driverState{}
	key := foundation.NewKey[*database.DB]("test.database")
	module := database.Module("test.database", key, func() (database.Adapter, error) { return database.Adapter{Connector: connector{state}}, nil }, database.DefaultPoolConfig())
	cause := errors.New("later boot failed")
	dependent := foundation.Module{Name: "test.dependent", Requires: []foundation.ProviderID{module.Name}, OnBoot: func(context.Context, *foundation.Runtime) error { return cause }}
	app, err := foundry.New().Register(dependent, module).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Start(t.Context()); !errors.Is(err, cause) {
		t.Fatal("boot failure lost")
	}
	if err := app.Shutdown(t.Context()); !errors.Is(err, cause) {
		t.Fatal("shutdown lost boot failure")
	}
	if state.closed.Load() != 1 {
		t.Fatal("partially booted pool leaked")
	}
}

func TestDatabaseModuleKeepsApplicationStoppingWhilePoolIsOwned(t *testing.T) {
	state := &driverState{}
	key := foundation.NewKey[*database.DB]("test.database")
	module := database.Module("test.database", key, func() (database.Adapter, error) { return database.Adapter{Connector: connector{state}}, nil }, database.DefaultPoolConfig())
	app, err := foundry.New(foundation.WithShutdownTimeout(5 * time.Millisecond)).Register(module).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	db, err := foundation.Resolve(app.Services(), key)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(context.Background(), "owned rows")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Millisecond)
	defer cancel()
	if err := app.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown passed open rows: %v", err)
	}
	if app.State() != foundation.Stopping {
		t.Fatal("application reported stopped prematurely")
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	_ = app.Shutdown(t.Context()) // the elapsed resource deadline remains reported
	if app.State() != foundation.Stopped || state.closed.Load() != 1 {
		t.Fatal("pool did not finish cleanup")
	}
}
