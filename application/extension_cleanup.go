package application

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions/slots"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

// extensionCleanupJob settles models deleted through a connection other than
// the extension store's when features.extension_cleanup names a job connection.
// Enqueueing selects that connection's default queue.
var extensionCleanupJob = slots.DefineCleanupJob("foundry.extensions.cleanup", jobs.DefaultPolicy("default"))

const extensionCleanupProvider foundation.ProviderID = "foundry.application.extension-cleanup"

var extensionCleanupKey = foundation.NewKey[*slots.CleanupQueue](string(extensionCleanupProvider))

// extensionCleanupDeclaration registers the cleanup job's handler on the
// configured connection. The handler uses the store and managers only, so it
// never depends on the queue that enqueues it.
func extensionCleanupDeclaration(connection jobs.ConnectionName) JobDeclaration {
	return Job(extensionCleanupJob.Definition(), func(services Services) (jobs.Handler[slots.CleanupRequest], error) {
		runtime, err := services.ModelExtensions()
		if err != nil {
			return nil, err
		}
		return extensionCleanupJob.Handler(runtime)
	}).On(connection)
}

// registerExtensionCleanup gives every configured pool a producer for the job
// connection's outbox destination, writing the outbox table the publisher
// relays. Boot verifies that every pool reaches the outbox's database, so a
// pool elsewhere fails startup instead of holding rows nothing publishes.
func registerExtensionCleanup(builder *foundation.Builder, s FeatureSettings, pools []database.ConnectionName) {
	name := s.ExtensionCleanup.Jobs
	// The outbox connection is one of the configured pools.
	requires := []foundation.ProviderID{infrastructure.JobProvider(name)}
	for _, pool := range pools {
		requires = append(requires, infrastructure.DatabaseProvider(pool))
	}
	builder.Register(foundation.Module{Name: extensionCleanupProvider, Requires: requires, OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, extensionCleanupKey, func(r foundation.Resolver) (*slots.CleanupQueue, error) {
			connection, err := foundation.Resolve(r, infrastructure.JobKey(name))
			if err != nil {
				return nil, err
			}
			if err := connection.Dispatcher().RequireDurable(); err != nil {
				return nil, err
			}
			producers := make(map[*database.DB]*jobs.Outbox, len(pools))
			for _, pool := range pools {
				db, err := foundation.Resolve(r, infrastructure.DatabaseKey(pool))
				if err != nil {
					return nil, err
				}
				if producers[db], err = jobs.PrepareOutboxIn(s.Outbox.Jobs[name], connection.Dispatcher(), db, s.Outbox.Schema); err != nil {
					return nil, err
				}
			}
			return extensionCleanupJob.ToOutbox(connection.DefaultQueue(), producers)
		})
	}, OnBoot: func(ctx context.Context, runtime *foundation.Runtime) error {
		outbox, err := foundation.Resolve(runtime.Services(), infrastructure.DatabaseKey(s.Outbox.Database))
		if err != nil {
			return err
		}
		want, err := databaseIdentity(ctx, outbox)
		if err != nil {
			return err
		}
		for _, pool := range pools {
			if pool == s.Outbox.Database {
				continue
			}
			db, err := foundation.Resolve(runtime.Services(), infrastructure.DatabaseKey(pool))
			if err != nil {
				return err
			}
			got, err := databaseIdentity(ctx, db)
			if err != nil {
				return err
			}
			if got != want {
				return fault.New(fault.Invalid, "database connection "+string(pool)+" does not reach the outbox database that features.extension_cleanup uses")
			}
		}
		return nil
	}})
}

// databaseIdentity names the server instance and database a pool reaches. The
// query runs on a routed pool's primary, and the start time is rendered in UTC
// with an explicit format, so session settings such as DateStyle cannot make
// two connections to one database differ.
func databaseIdentity(ctx context.Context, db *database.DB) (string, error) {
	var name, started string
	err := database.ScanOne(ctx, db, `SELECT pg_catalog.current_database()::text, pg_catalog.to_char(pg_catalog.pg_postmaster_start_time() AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS.US')`, nil, &name, &started)
	return name + "\x00" + started, err
}
