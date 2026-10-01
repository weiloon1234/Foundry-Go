package plugin_test

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/plugin"
)

func TestPluginMigrationsKeepHistoricalReleaseAndOwnedSQL(t *testing.T) {
	key := foundation.NewKey[*migrate.Registry]("plugin.migrations")
	baseKey := migrate.Key{Origin: plugin.MigrationOrigin("base"), ID: "001_base"}
	base := plugin.Module{Declaration: plugin.Manifest{ID: "base", Version: "2.0.0", Framework: "*"}}
	base.OnRegister = func(r *plugin.Registrar) error {
		return plugin.RegisterMigrations(r, key, migrate.Definition{Key: baseKey, Version: "1.0.0", SQL: []string{"SELECT 1"}})
	}
	childDefinition := migrate.Definition{Key: migrate.Key{ID: "002_child"}, Version: "1.1.0", SQL: []string{"SELECT 2"}, Requires: []migrate.Key{baseKey}}
	child := plugin.Module{Declaration: plugin.Manifest{ID: "child", Version: "1.2.0", Framework: "*", Dependencies: []plugin.Dependency{{ID: "base", Version: "^2.0.0"}}}, OnRegister: func(r *plugin.Registrar) error { return plugin.RegisterMigrations(r, key, childDefinition) }}
	module := foundation.Module{Name: "migrations", OnRegister: func(r *foundation.Registrar) error { return plugin.RegisterMigrationRegistry(r, key) }}
	app, err := foundation.NewBuilder().Register(module).RegisterPlugin(child, base).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := foundation.Resolve(app.Services(), key)
	if err != nil {
		t.Fatal(err)
	}
	entries := registry.Entries()
	if len(entries) != 2 || entries[0].Key != baseKey || entries[0].Version != "1.0.0" || entries[1].Key.Origin != plugin.MigrationOrigin("child") {
		t.Fatalf("historical plugin migrations: %+v", entries)
	}
	childDefinition.SQL[0] = "SELECT 3"
	childDefinition.Requires[0].ID = "changed"
	if registry.Entries()[1].Checksum != entries[1].Checksum || registry.Entries()[1].Requires[0] != baseKey {
		t.Fatal("registry retained mutable migration inputs")
	}
	for _, definition := range []migrate.Definition{
		{Key: migrate.Key{Origin: "application", ID: "foreign"}, Version: "1.0.0", SQL: []string{"SELECT 1"}},
		{Key: migrate.Key{ID: "future"}, Version: "3.0.0", SQL: []string{"SELECT 1"}},
		{Key: migrate.Key{ID: "invalid"}, Version: "1", SQL: []string{"SELECT 1"}},
	} {
		bad := base
		bad.OnRegister = func(r *plugin.Registrar) error { return plugin.RegisterMigrations(r, key, definition) }
		if _, err := foundation.NewBuilder().Register(module).RegisterPlugin(bad).Build(t.Context()); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid plugin migration accepted", err)
		}
	}
}

func TestPluginMigrationsSnapshotDownSQL(t *testing.T) {
	key := foundation.NewKey[*migrate.Registry]("plugin.migrations")
	appDefinition := migrate.Definition{Key: migrate.Key{Origin: "app", ID: "000_app"}, Version: "1.0.0", SQL: []string{"SELECT 0"}, Down: []string{"SELECT 0"}}
	pluginDefinition := migrate.Definition{Key: migrate.Key{ID: "001_base"}, Version: "1.0.0", SQL: []string{"SELECT 1"}, Down: []string{"SELECT 1"}}
	// Blank Down SQL is invalid, so a registry still sharing caller storage fails to build.
	base := plugin.Module{Declaration: plugin.Manifest{ID: "base", Version: "1.0.0", Framework: "*"}, OnRegister: func(r *plugin.Registrar) error {
		err := plugin.RegisterMigrations(r, key, pluginDefinition)
		pluginDefinition.Down[0] = " "
		return err
	}}
	module := foundation.Module{Name: "migrations", OnRegister: func(r *foundation.Registrar) error {
		err := plugin.RegisterMigrationRegistry(r, key, appDefinition)
		appDefinition.Down[0] = " "
		return err
	}}
	app, err := foundation.NewBuilder().Register(module).RegisterPlugin(base).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := foundation.Resolve(app.Services(), key)
	if err != nil {
		t.Fatal("registry retained mutable Down SQL: ", err)
	}
	entries := registry.Entries()
	if len(entries) != 2 || !entries[0].Reversible || !entries[1].Reversible {
		t.Fatalf("registered Down SQL lost: %+v", entries)
	}
}
