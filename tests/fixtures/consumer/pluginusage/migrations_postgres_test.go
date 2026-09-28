package pluginusage_test

import (
	"testing"

	"foundry.test/consumer/pluginusage"
	"foundry.test/pluginbase"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/foundation"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestPostgresIndependentPluginMigrationHistory(t *testing.T) {
	db := pgtest.Open(t)
	builder, err := pluginusage.Builder("history")
	if err != nil {
		t.Fatal(err)
	}
	app, err := builder.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := foundation.Resolve(app.Services(), pluginbase.Migrations)
	if err != nil {
		t.Fatal(err)
	}
	config := migrate.DefaultPostgresConfig()
	config.Schema = pgtest.Namespace(t, db)
	runner, err := migrate.NewPostgres(db, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	before, err := runner.Status(t.Context())
	if err != nil || len(before.Statuses) != 2 {
		t.Fatal(before, err)
	}
	first, err := runner.Up(t.Context())
	if err != nil || len(first.Applied) != 2 || first.Applied[0].Version != "1.0.0" || first.Applied[1].Version != "1.0.0" {
		t.Fatal("versioned migration application", first, err)
	}
	second, err := runner.Up(t.Context())
	if err != nil || len(second.Applied) != 0 {
		t.Fatal("plugin migration reapplied", second, err)
	}
	after, err := runner.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := after.Check(); err != nil {
		t.Fatal(err)
	}
	if pending, err := registry.Pending(first.Applied); err != nil || len(pending) != 0 {
		t.Fatal("historical plugin identities lost", pending, err)
	}
	// Inspect corrupt history without modifying the actual retained database.
	history := append([]migrate.Applied(nil), first.Applied...)
	history[0].Version = "1.0.1"
	drift, err := registry.Inspect(history)
	if err != nil {
		t.Fatal(err)
	}
	if err := drift.Check(); err == nil {
		t.Fatal("introduced version drift accepted")
	}
}
