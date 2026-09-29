package application

import (
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/jobs/archive"
	"slices"
)

func featureMigrations(plan *infrastructure.Plan, s FeatureSettings, jobArchive JobArchiveSettings) ([]infrastructure.MigrationTarget, error) {
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
	for _, t := range persistenceTargets(&s) {
		if t.definitions != nil {
			add(*t.connection, *t.schema, t.definitions())
		}
	}
	if jobArchive.Enabled {
		add(jobArchive.Database, jobArchive.Schema, archive.Migrations())
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
	result := make([]infrastructure.MigrationTarget, 0, len(keys))
	for _, key := range keys {
		definitions := groups[key]
		if _, err := migrate.New(definitions...); err != nil {
			return nil, err
		}
		result = append(result, infrastructure.MigrationTarget{Connection: key.connection, Schema: key.schema, Definitions: definitions})
	}
	return result, nil
}
func cloneMigrations(input []infrastructure.MigrationTarget) []infrastructure.MigrationTarget {
	result := slices.Clone(input)
	for i := range result {
		result[i].Definitions = slices.Clone(result[i].Definitions)
		for j := range result[i].Definitions {
			d := &result[i].Definitions[j]
			d.SQL = slices.Clone(d.SQL)
			d.Requires = slices.Clone(d.Requires)
		}
	}
	return result
}
