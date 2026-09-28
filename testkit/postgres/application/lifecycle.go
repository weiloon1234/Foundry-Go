package application

import (
	"context"
	"errors"
	"reflect"
	"slices"
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
	Migrations() []infrastructure.MigrationTarget
}

// Start owns cleanup before migration/startup. Framework and explicit domain
// migrations commit through the ordinary runner before providers start. A failed
// attempt also shuts down immediately. HTTP/worker lifetimes remain app-owned;
// register test clients afterward so their cleanup precedes app shutdown.
func (e *Environment) Start(t testing.TB, app Application, domain ...infrastructure.MigrationTarget) error {
	t.Helper()
	if app == nil || (reflect.ValueOf(app).Kind() == reflect.Pointer && reflect.ValueOf(app).IsNil()) || app.ShutdownTimeout() <= 0 {
		return fault.New(fault.Invalid, "test environment requires a prepared application")
	}
	stop := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), app.ShutdownTimeout())
		defer cancel()
		return app.Shutdown(ctx)
	}
	var cleanup sync.Once
	closeFailed := func(cause error) error {
		var shutdown error
		cleanup.Do(func() { shutdown = stop() })
		return errors.Join(cause, shutdown)
	}
	t.Cleanup(func() {
		cleanup.Do(func() {
			if err := stop(); err != nil {
				t.Errorf("shutdown scoped application: %v", err)
			}
		})
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

type migrationGroup struct {
	schema   string
	config   postgres.Config
	registry *migrate.Registry
}

func (e *Environment) migrationGroups(targets []infrastructure.MigrationTarget) ([]migrationGroup, error) {
	if e == nil || len(e.settings.Connections) == 0 {
		return nil, fault.New(fault.Invalid, "uninitialized PostgreSQL test environment")
	}
	definitions := make(map[string]map[migrate.Key]migrate.Definition)
	configs := make(map[string]postgres.Config)
	for _, target := range targets {
		name, schema, err := e.settings.ScopedSchema(target.Connection)
		if err != nil {
			return nil, err
		}
		if target.Schema != "" && target.Schema != schema {
			return nil, fault.New(fault.Invalid, "migration target escapes its test namespace")
		}
		if definitions[schema] == nil {
			definitions[schema] = make(map[migrate.Key]migrate.Definition)
			configs[schema] = e.settings.Connections[name].Primary.Config()
		}
		for _, d := range target.Definitions {
			// Sharing a physical namespace may repeat an identical framework migration.
			// Conflicting definitions still fail before opening a pool or applying SQL.
			d.SQL = slices.Clone(d.SQL)
			d.Requires = slices.Clone(d.Requires)
			if previous, ok := definitions[schema][d.Key]; ok && !reflect.DeepEqual(previous, d) {
				return nil, fault.New(fault.Conflict, "shared test namespace has conflicting migration definitions")
			}
			definitions[schema][d.Key] = d
		}
	}
	schemas := make([]string, 0, len(definitions))
	for schema := range definitions {
		schemas = append(schemas, schema)
	}
	slices.Sort(schemas)
	groups := make([]migrationGroup, 0, len(schemas))
	for _, schema := range schemas {
		var ds []migrate.Definition
		for _, d := range definitions[schema] {
			ds = append(ds, d)
		}
		registry, err := migrate.New(ds...)
		if err != nil {
			return nil, err
		}
		groups = append(groups, migrationGroup{schema, configs[schema], registry})
	}
	return groups, nil
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
func (e *Environment) migrateGroup(ctx context.Context, group migrationGroup) (err error) {
	db, err := postgres.Open(ctx, group.config)
	if err != nil {
		return err
	}
	c := migrate.DefaultPostgresConfig()
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), c.CleanupTimeout)
		defer cancel()
		err = errors.Join(err, db.Close(cleanup))
	}()
	c.Schema = group.schema
	runner, err := migrate.NewPostgres(db, group.registry, c)
	if err != nil {
		return err
	}
	_, err = runner.Up(ctx)
	return err
}
