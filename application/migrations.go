package application

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// maxMigrationTargets bounds the application's own migration declarations.
const maxMigrationTargets = 128

// Migrations adds the application's own historical definitions. A blank
// Connection selects the default connection and a blank Schema that
// connection's schema (public when unscoped). The targets are snapshotted,
// join the framework definitions in App.Migrations and App.RunDatabaseCommand,
// are validated with them at Build, and are never applied during boot.
func (b *Builder) Migrations(targets ...infrastructure.MigrationTarget) *Builder {
	b.mutate(func() {
		if len(targets) > maxMigrationTargets-len(b.state.migrations) {
			b.state.err = safeerror.NewFault(fault.Invalid, "too many application migration targets")
			return
		}
		b.state.migrations = append(b.state.migrations, cloneMigrations(targets)...)
	})
	return b
}

// featureMigrations uses the shared grouping owner to merge framework and
// application declarations into one immutable registry per physical history.
func featureMigrations(plan *infrastructure.Plan, s Settings, domain []infrastructure.MigrationTarget) ([]applicationMigrationGroup, error) {
	targets := plan.Migrations()
	for _, target := range persistenceTargets(&s) {
		if target.definitions == nil || (!target.enabled && len(s.Services.Database.Connections) == 0) {
			continue
		}
		targets = append(targets, infrastructure.MigrationTarget{
			Connection: *target.connection, Schema: *target.schema,
			Definitions: migrate.FeatureDefinitions(target.enabled, target.definitions()...),
		})
	}
	// Build already snapshotted the domain targets under the builder lock.
	targets = append(targets, domain...)
	resolved, err := s.Services.Database.MigrationGroups(targets...)
	if err != nil {
		return nil, err
	}
	var history []applicationMigrationGroup
	for _, group := range resolved {
		registry, err := migrate.New(group.Definitions...)
		if err != nil {
			return nil, err
		}
		history = append(history, applicationMigrationGroup{group, registry})
	}
	return history, nil
}

// Each immutable registry owns both execution and history inspection for one
// physical target. The group retains every configured alias for command routing.
type applicationMigrationGroup struct {
	infrastructure.MigrationGroup
	registry *migrate.Registry
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
