package infrastructure

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

// MigrationGroup is one migration history: every target addressing the same
// configured primary database endpoint and schema, merged into one definition
// set. Connections lists every configured name that addresses it, sorted; any
// of them can run the group because they reach the same database and schema.
type MigrationGroup struct {
	Connections []database.ConnectionName
	Schema      string
	Definitions []migrate.Definition
}

// PostgresConfig keeps the group's history in its own schema
// (<schema>.<base.Table>, by default <schema>.schema_migrations) and runs its
// unqualified SQL with that schema as the search path. Other bounds come from base.
func (g MigrationGroup) PostgresConfig(base migrate.PostgresConfig) migrate.PostgresConfig {
	base.Schema, base.SearchPath = g.Schema, g.Schema
	return base
}

type migrationEndpoint struct {
	host     string
	port     uint16
	database string
	schema   string
}

// MigrationGroups resolves every target before any database I/O. A blank
// connection selects the default alias and a blank schema selects the
// connection's primary schema, or public for an unscoped connection. Targets
// addressing the same primary host, port, database and schema share one history
// table, so they merge into one group: an identical repeated definition collapses
// and a conflicting one fails. Each group must form a valid migrate registry.
// Targets without definitions contribute nothing. Groups are ordered by first
// connection name, then schema; definitions keep their first-seen order and are
// owned copies.
func (s DatabaseSettings) MigrationGroups(targets ...MigrationTarget) ([]MigrationGroup, error) {
	var groups []MigrationGroup
	byEndpoint := make(map[migrationEndpoint]int)
	byKey := make([]map[migrate.Key]int, 0)
	for _, target := range targets {
		name := target.Connection
		if name == "" {
			name = s.Default
		}
		connection, ok := s.Connections[name]
		if !ok {
			return nil, fault.New(fault.Missing, "migration target database is not configured")
		}
		schema := target.Schema
		if schema == "" {
			schema = connection.Primary.Schema
		}
		if schema == "" {
			schema = "public"
		}
		if !sqlname.Valid(schema) {
			return nil, fault.New(fault.Invalid, "migration target requires a valid PostgreSQL schema")
		}
		if len(target.Definitions) == 0 {
			continue
		}
		endpoint := migrationEndpoint{connection.Primary.Host, connection.Primary.Port, connection.Primary.Database, schema}
		index, exists := byEndpoint[endpoint]
		if !exists {
			index = len(groups)
			byEndpoint[endpoint] = index
			groups = append(groups, MigrationGroup{Schema: schema})
			byKey = append(byKey, make(map[migrate.Key]int))
		}
		group := &groups[index]
		if !slices.Contains(group.Connections, name) {
			group.Connections = append(group.Connections, name)
		}
		for _, definition := range target.Definitions {
			definition = definition.Clone()
			if previous, seen := byKey[index][definition.Key]; seen {
				// Features sharing one schema may repeat the same framework migration.
				if !reflect.DeepEqual(group.Definitions[previous], definition) {
					return nil, fault.New(fault.Conflict, fmt.Sprintf("migration targets sharing schema %s have conflicting definitions for %s/%s", schema, definition.Key.Origin, definition.Key.ID))
				}
				continue
			}
			byKey[index][definition.Key] = len(group.Definitions)
			group.Definitions = append(group.Definitions, definition)
		}
	}
	for i := range groups {
		slices.Sort(groups[i].Connections)
		if _, err := migrate.New(groups[i].Definitions...); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(groups, func(a, b MigrationGroup) int {
		if order := strings.Compare(string(a.Connections[0]), string(b.Connections[0])); order != 0 {
			return order
		}
		return strings.Compare(a.Schema, b.Schema)
	})
	return groups, nil
}
