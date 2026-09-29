package infrastructure_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	cachepg "github.com/weiloon1234/Foundry-Go/cache/postgres"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestPostgresCacheFollowsApplicationLifecycle(t *testing.T) {
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`", pg_temp`); err != nil {
			return err
		}
		for _, d := range cachepg.Migrations() {
			for _, sql := range d.SQL {
				if _, err := tx.Exec(t.Context(), sql); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s := memorySettings()
	connection := infrastructure.DefaultConnectionSettings()
	connection.Primary = infrastructure.PostgreSQLSettingsFromConfig(pgtest.Config(t))
	s.Database.Connections = infrastructure.DatabaseConnections{"default": connection}
	persistent := infrastructure.DefaultCacheSettings()
	persistent.Driver = infrastructure.PostgresCache
	persistent.Postgres.Schema = schema
	persistent.Postgres.PruneInterval = time.Second
	s.Cache.Stores["second"] = persistent
	plan, err := infrastructure.Configure(s)
	if err != nil {
		t.Fatal(err)
	}
	app := built(t, plan)
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	store, err := services(t, app).Caches.Store("second")
	if err != nil {
		t.Fatal(err)
	}
	bound, err := values.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := bound.Put(t.Context(), "one", "persisted", cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if value, hit, err := bound.Get(t.Context(), "one"); err != nil || !hit || value != "persisted" {
		t.Fatal(value, hit, err)
	}
	// Shutdown stops the owned pruner and drains the cache before the
	// database it borrows closes.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := bound.Get(t.Context(), "one"); !errors.Is(err, fault.Closed) {
		t.Fatal("PostgreSQL cache stayed open", err)
	}
}

// The null driver builds a store that satisfies every capability requirement
// except distributed fills and retains nothing.
func TestNullCacheDriverRetainsNothing(t *testing.T) {
	s := memorySettings()
	disabled := infrastructure.DefaultCacheSettings()
	disabled.Driver = infrastructure.NullCache
	disabled.Require = cache.Requirements{Tags: true, Counters: true, Entries: true, Batches: true}
	s.Cache.Stores["second"] = disabled
	plan, err := infrastructure.Configure(s)
	if err != nil {
		t.Fatal(err)
	}
	app := built(t, plan)
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	store, err := services(t, app).Caches.Store("second")
	if err != nil {
		t.Fatal(err)
	}
	bound, err := values.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := bound.Put(t.Context(), "one", "discarded", cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := bound.Get(t.Context(), "one"); err != nil || hit {
		t.Fatal("null cache returned a value", hit, err)
	}
	disabled.Require = cache.Requirements{DistributedFills: true}
	s.Cache.Stores["second"] = disabled
	if _, err := infrastructure.Configure(s); !errors.Is(err, fault.Invalid) {
		t.Fatal("null cache accepted distributed fills", err)
	}
}

// Application shutdown drains a store's in-flight Flexible refresh before the
// cache backend closes.
func TestShutdownWaitsForInFlightCacheRefresh(t *testing.T) {
	plan, err := infrastructure.Configure(memorySettings())
	if err != nil {
		t.Fatal(err)
	}
	app, err := plan.Register(foundation.NewBuilder()).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	store, err := services(t, app).Caches.Store("second")
	if err != nil {
		t.Fatal(err)
	}
	bound, err := values.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bound.Flexible(t.Context(), "refreshed", time.Millisecond, time.Hour, func(context.Context) (string, error) { return "old", nil }); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	entered, release := make(chan struct{}), make(chan struct{})
	var exited atomic.Bool
	value, err := bound.Flexible(t.Context(), "refreshed", time.Millisecond, time.Hour, func(context.Context) (string, error) {
		close(entered)
		<-release // ignores cancellation, like a slow loader
		exited.Store(true)
		return "new", nil
	})
	if err != nil || value != "old" {
		t.Fatal(value, err)
	}
	<-entered
	stopped := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		stopped <- app.Shutdown(ctx)
	}()
	select {
	case err := <-stopped:
		t.Fatal("shutdown finished while a cache refresh was running", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if !exited.Load() {
		t.Fatal("shutdown returned before the refresh exited")
	}
}
