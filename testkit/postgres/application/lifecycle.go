package application

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
)

// Application is satisfied by *application.App without importing application
// assembly into a feature helper. Build it from Environment.Settings through
// application.Settings.WithDatabaseScopes before handing it to Start.
type Application interface {
	Start(context.Context) error
	Shutdown(context.Context) error
	ShutdownTimeout() time.Duration
	// Done closes only after all owned work and dependency cleanup finish.
	Done() <-chan struct{}
	Migrations() []infrastructure.MigrationTarget
}

// Start owns cleanup before migration/startup. Framework and explicit domain
// migrations commit through the ordinary runner before providers start. A failed
// attempt also shuts down immediately. HTTP/worker lifetimes remain app-owned;
// register test clients afterward so their cleanup precedes app shutdown.
//
// Concurrency is bounded to DefaultConcurrency (8) per test process. All apps
// started within a single top-level test (including subtests) share that test's
// single concurrency slot. The slot is released after the application has
// completely shut down.
func (e *Environment) Start(t testing.TB, app Application, domain ...infrastructure.MigrationTarget) error {
	t.Helper()
	if app == nil || (reflect.ValueOf(app).Kind() == reflect.Pointer && reflect.ValueOf(app).IsNil()) || app.ShutdownTimeout() <= 0 {
		return fault.New(fault.Invalid, "test environment requires a prepared application")
	}
	done := app.Done()
	if done == nil {
		return fault.New(fault.Invalid, "test application requires a completion signal")
	}
	release, err := acquireSlot(t)
	if err != nil {
		return err
	}
	stop := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), app.ShutdownTimeout())
		defer cancel()
		return app.Shutdown(ctx)
	}
	var cleanup sync.Once
	closeCleanup := func() (err error) {
		cleanup.Do(func() {
			err = stop()
			select {
			case <-done:
				release()
			default:
				// A shutdown deadline bounds the caller's wait, not resource
				// ownership. Keep capacity until actual cleanup completion.
				go func() {
					<-done
					release()
				}()
			}
		})
		return err
	}
	closeFailed := func(cause error) error {
		return errors.Join(cause, closeCleanup())
	}
	t.Cleanup(func() {
		if err := closeCleanup(); err != nil {
			t.Errorf("shutdown scoped application: %v", err)
		}
	})
	started := time.Now()
	targets := append(app.Migrations(), domain...)
	if err := e.Migrate(t.Context(), targets...); err != nil {
		return closeFailed(err)
	}
	t.Logf("Scoped migration preparation: %s", time.Since(started))
	if err := app.Start(t.Context()); err != nil {
		return closeFailed(err)
	}
	return nil
}

// migrationGroups keeps every target inside its test namespace, then reuses the
// application grouping: one registry and history per physical namespace.
func (e *Environment) migrationGroups(targets []infrastructure.MigrationTarget) ([]infrastructure.MigrationGroup, error) {
	if e == nil || len(e.settings.Connections) == 0 {
		return nil, fault.New(fault.Invalid, "uninitialized PostgreSQL test environment")
	}
	for _, target := range targets {
		_, schema, err := e.settings.ScopedSchema(target.Connection)
		if err != nil {
			return nil, err
		}
		if target.Schema != "" && target.Schema != schema {
			return nil, fault.New(fault.Invalid, "migration target escapes its test namespace")
		}
	}
	return e.settings.MigrationGroups(targets...)
}

// Migrate validates all targets first, then applies one registry/history per
// physical namespace. Each short-lived primary pool closes before the next opens.
// Successful migrations and data remain committed if a later group fails.
func (e *Environment) Migrate(ctx context.Context, targets ...infrastructure.MigrationTarget) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "test migration requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	groups, err := e.migrationGroups(targets)
	if err != nil {
		return err
	}
	for _, group := range groups {
		if err := e.migrateGroup(ctx, group); err != nil {
			return err
		}
	}
	return nil
}
func (e *Environment) migrateGroup(ctx context.Context, group infrastructure.MigrationGroup) (err error) {
	registry, err := migrate.New(group.Definitions...)
	if err != nil {
		return err
	}
	db, err := postgres.Open(ctx, e.settings.Connections[group.Connections[0]].Primary.Config())
	if err != nil {
		return err
	}
	c := migrate.DefaultPostgresConfig()
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), c.CleanupTimeout)
		defer cancel()
		err = errors.Join(err, db.Close(cleanup))
	}()
	runner, err := migrate.NewPostgres(db, registry, group.PostgresConfig(c))
	if err != nil {
		return err
	}
	_, err = runner.Up(ctx)
	return err
}
