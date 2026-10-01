package application

import (
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/jobs/archive"
	"slices"
)

// maxMigrationTargets bounds the application's own migration declarations.
const maxMigrationTargets = 128

// Migrations adds the application's own historical definitions. A blank
// Connection selects the default connection and a blank Schema that
// connection's schema (public when unscoped). The targets are snapshotted,
// join App.Migrations() and App.RunDatabaseCommand after the framework feature
// targets, are validated with them at Build, and are never applied during boot.
func (b *Builder) Migrations(targets ...infrastructure.MigrationTarget) *Builder {
	b.mutate(func() {
		if len(targets) > maxMigrationTargets-len(b.state.migrations) {
			b.state.err = fault.New(fault.Invalid, "too many application migration targets")
			return
		}
		b.state.migrations = append(b.state.migrations, cloneMigrations(targets)...)
	})
	return b
}

// featureMigrations collects enabled framework feature targets by connection and
// schema, then appends the application's own targets. Every target is resolved
// and validated through the shared migration grouping before any I/O.
func featureMigrations(plan *infrastructure.Plan, s Settings, domain []infrastructure.MigrationTarget) ([]infrastructure.MigrationTarget, error) {
	type target struct {
		connection database.ConnectionName
		schema     string
	}
	groups := make(map[target][]migrate.Definition)
	add := func(connection database.ConnectionName, schema string, definitions []migrate.Definition) {
		key := target{connection, schema}
		groups[key] = append(groups[key], definitions...)
	}
	for _, t := range plan.Migrations() {
		add(t.Connection, t.Schema, t.Definitions)
	}
	features := s.Features
	for _, t := range persistenceTargets(&features) {
		if t.definitions != nil {
			add(*t.connection, *t.schema, t.definitions())
		}
	}
	if s.Worker.Archive.Enabled {
		add(s.Worker.Archive.Database, s.Worker.Archive.Schema, archive.Migrations())
	}
	keys := make([]target, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b target) int {
		if a.connection < b.connection {
			return -1
		}
		if a.connection > b.connection {
			return 1
		}
		if a.schema < b.schema {
			return -1
		}
		if a.schema > b.schema {
			return 1
		}
		return 0
	})
	result := make([]infrastructure.MigrationTarget, 0, len(keys)+len(domain))
	for _, key := range keys {
		result = append(result, infrastructure.MigrationTarget{Connection: key.connection, Schema: key.schema, Definitions: groups[key]})
	}
	// Build already snapshotted the domain targets under the builder lock.
	result = append(result, domain...)
	if _, err := s.Services.Database.MigrationGroups(result...); err != nil {
		return nil, err
	}
	return result, nil
}
func cloneMigrations(input []infrastructure.MigrationTarget) []infrastructure.MigrationTarget {
	result := slices.Clone(input)
	for i := range result {
		result[i].Definitions = slices.Clone(result[i].Definitions)
		for j := range result[i].Definitions {
			result[i].Definitions[j] = result[i].Definitions[j].Clone()
		}
	}
	return result
}
