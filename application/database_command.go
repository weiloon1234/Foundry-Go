package application

import (
	"context"
	"errors"
	"io"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	dbcommand "github.com/weiloon1234/Foundry-Go/database/command"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/database/prune"
	"github.com/weiloon1234/Foundry-Go/database/seed"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
)

// DatabaseCommandResources supplies the application registries used by the
// seed and prune commands. Both are optional; migration commands use neither.
type DatabaseCommandResources struct {
	Seeders   *seed.Registry
	Prunables *prune.Registry
}

// RunDatabaseCommand runs one parsed database command against the connection it
// selects with --database, or the default connection. It needs only a built
// application: it never starts providers, prepares just that connection's
// primary pool and closes it before returning, so unrelated services need not
// be reachable. The pool connects only for a command that needs the database;
// migrate show, seed list and prune list read their registries without I/O.
// It uses the application's encryption key ring, if any, for encrypted model
// fields. Every App.Migrations() group on that database gets a runner that
// keeps history in <schema>.schema_migrations and executes with that schema as
// its search path; --schema selects one. Status stays read-only and creates no
// history table. Up runs the schemas in order and stops at the first failure.
func (a *App) RunDatabaseCommand(ctx context.Context, invocation dbcommand.Command, resources DatabaseCommandResources, output io.Writer) (err error) {
	if a == nil || ctx == nil {
		return fault.New(fault.Invalid, "database command requires a built application and context")
	}
	name := invocation.Database()
	if name == "" {
		name = a.databases.Default
	}
	connection, ok := a.databases.Connections[name]
	if !ok {
		return fault.New(fault.Missing, "selected database connection is not configured")
	}
	groups, err := a.databases.MigrationGroups(a.migrations...)
	if err != nil {
		return err
	}
	var selected []infrastructure.MigrationGroup
	var registries []*migrate.Registry
	for _, group := range groups {
		if !slices.Contains(group.Connections, name) {
			continue
		}
		registry, err := migrate.New(group.Definitions...)
		if err != nil {
			return err
		}
		selected, registries = append(selected, group), append(registries, registry)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	pool := connection.Primary.Config()
	adapter, err := postgres.New(pool)
	if err != nil {
		return err
	}
	// The pool shares the application key ring, so seeders and prunables can
	// write and hydrate encrypted model fields as they do in the running app.
	var options []database.Option
	if a.encryption != nil {
		options = append(options, database.WithEncryption(a.encryption))
	}
	db, err := database.Prepare(adapter, pool.Pool, options...)
	if err != nil {
		return err
	}
	config := migrate.DefaultPostgresConfig()
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), config.CleanupTimeout)
		defer cancel()
		err = errors.Join(err, db.Close(cleanup))
	}()
	if invocation.NeedsDatabase() {
		if err := db.Start(ctx); err != nil {
			return err
		}
	}
	targets := make([]dbcommand.MigrationTarget, 0, len(registries))
	for i, group := range selected {
		runner, err := migrate.NewPostgres(db, registries[i], group.PostgresConfig(config))
		if err != nil {
			return err
		}
		targets = append(targets, dbcommand.MigrationTarget{Schema: group.Schema, Runner: runner})
	}
	return invocation.Run(ctx, dbcommand.Resources{MigrationTargets: targets, Seeders: resources.Seeders, Prunables: resources.Prunables, Database: db, DatabaseName: name}, output)
}
