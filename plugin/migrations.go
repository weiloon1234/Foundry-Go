package plugin

import (
	"fmt"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

// MigrationOrigin is stable across plugin releases. Individual definitions keep
// the release that introduced them, rather than the currently installed version.
func MigrationOrigin(id ID) migrate.Origin { return migrate.Origin("plugin." + string(id)) }

type migrationRegistryRegistration struct{}

func migrationRegistryKey(key foundation.Key[*migrate.Registry]) foundation.Key[migrationRegistryRegistration] {
	return foundation.NewKey[migrationRegistryRegistration](fmt.Sprintf("foundry.plugin.migrations.%q", key.Name()))
}

func migrationContributions(key foundation.Key[*migrate.Registry]) foundation.Collection[migrate.Definition] {
	return foundation.NewCollection[migrate.Definition](fmt.Sprintf("plugin.migrations.%q", key.Name()))
}

// RegisterMigrations snapshots historical SQL and dependencies under this
// plugin's origin. An omitted origin is filled; a foreign origin is rejected.
// Version is the full semantic release that introduced each migration and must
// not exceed the captured installed version. No database I/O runs here or on boot.
func RegisterMigrations(r *Registrar, key foundation.Key[*migrate.Registry], definitions ...migrate.Definition) error {
	declaration, ok := r.Plugin()
	if !ok || key.Name() == "" {
		return fault.New(fault.Invalid, "plugin migrations require a plugin registrar and registry key")
	}
	origin := MigrationOrigin(declaration.ID)
	for _, definition := range definitions {
		if definition.Key.Origin == "" {
			definition.Key.Origin = origin
		}
		if definition.Key.Origin != origin {
			return fault.New(fault.Invalid, "plugin migration has a foreign origin")
		}
		order, err := Version(definition.Version).Compare(declaration.Version)
		if err != nil {
			return err
		}
		if order > 0 {
			return fault.New(fault.Invalid, "plugin migration was introduced after the installed plugin version")
		}
		definition = definition.Clone()
		if err := foundation.Contribute(r, migrationContributions(key), fmt.Sprintf("%q.%q", definition.Key.Origin, definition.Key.ID), func(resolver foundation.Resolver) (migrate.Definition, error) {
			if _, err := foundation.Resolve(resolver, migrationRegistryKey(key)); err != nil {
				return migrate.Definition{}, err
			}
			return definition, nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// RegisterMigrationRegistry combines explicit app/framework history with plugin
// history through migrate.New, preserving its checksum and dependency semantics.
// Resolve the registry for an explicit migrate runner; it never runs implicitly.
func RegisterMigrationRegistry(r *Registrar, key foundation.Key[*migrate.Registry], definitions ...migrate.Definition) error {
	if err := foundation.Provide(r, migrationRegistryKey(key), migrationRegistryRegistration{}); err != nil {
		return err
	}
	owned := make([]migrate.Definition, len(definitions))
	for i, definition := range definitions {
		owned[i] = definition.Clone()
	}
	return foundation.Factory(r, key, func(resolver foundation.Resolver) (*migrate.Registry, error) {
		plugins, err := foundation.Contributions(resolver, migrationContributions(key))
		if err != nil {
			return nil, err
		}
		return migrate.New(append(slices.Clone(owned), plugins...)...)
	})
}
