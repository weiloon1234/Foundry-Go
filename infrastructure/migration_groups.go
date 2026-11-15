package infrastructure

import (
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
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
	for _, target := range targets {
		name := target.Connection
		if name == "" {
			name = s.Default
		}
		connection, ok := s.Connections[name]
		if !ok {
			return nil, safeerror.NewFault(fault.Missing, "migration target database is not configured")
		}
		schema := target.Schema
		if schema == "" {
			schema = connection.Primary.Schema
		}
		if schema == "" {
			schema = "public"
		}
		if !sqlname.Valid(schema) {
			return nil, safeerror.NewFault(fault.Invalid, "migration target requires a valid PostgreSQL schema")
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
		}
		group := &groups[index]
		if !slices.Contains(group.Connections, name) {
			group.Connections = append(group.Connections, name)
		}
		group.Definitions = append(group.Definitions, target.Definitions...)
	}
	for i := range groups {
		slices.Sort(groups[i].Connections)
		definitions, err := migrate.MergeDefinitions(groups[i].Definitions...)
		if err != nil {
			return nil, err
		}
		if _, err := migrate.New(definitions...); err != nil {
			return nil, err
		}
		groups[i].Definitions = definitions
	}

	slices.SortFunc(groups, func(a, b MigrationGroup) int {
		if order := strings.Compare(string(a.Connections[0]), string(b.Connections[0])); order != 0 {
			return order
		}
		return strings.Compare(a.Schema, b.Schema)
	})
	return groups, nil
}
