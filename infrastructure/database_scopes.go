package infrastructure

import (
	"github.com/weiloon1234/Foundry-Go/cache"
	cachepg "github.com/weiloon1234/Foundry-Go/cache/postgres"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"maps"
)

// ScopedSchema resolves the default alias and rejects an unscoped endpoint.
func (s DatabaseSettings) ScopedSchema(name database.ConnectionName) (database.ConnectionName, string, error) {
	if name == "" {
		name = s.Default
	}
	c, ok := s.Connections[name]
	if !ok {
		return "", "", fault.New(fault.Missing, "database scope is not configured")
	}
	if err := c.Validate(); err != nil {
		return "", "", err
	}
	if c.Primary.Schema == "" || (c.ReadEnabled && c.Read.Schema != c.Primary.Schema) {
		return "", "", fault.New(fault.Invalid, "database scope requires matching primary and read schemas")
	}
	return name, c.Primary.Schema, nil
}

type cacheTarget struct {
	name cache.StoreName
	MigrationTarget
}

func cacheTargets(s Settings) []cacheTarget {
	var result []cacheTarget
	for _, name := range keys(s.Cache.Stores) {
		c := s.Cache.Stores[name]
		if c.Driver == PostgresCache {
			result = append(result, cacheTarget{name, MigrationTarget{Connection: c.Database, Schema: c.Postgres.Schema, Definitions: cachepg.Migrations()}})
		}
	}
	return result
}

// WithDatabaseScopes returns independently owned database/cache maps. A blank or
// public feature schema means the conventional default; other explicit schemas
// must already match their selected connection. No I/O or migrations run here.
func (s Settings) WithDatabaseScopes(databases DatabaseSettings) (Settings, error) {
	databases.Connections = maps.Clone(databases.Connections)
	if len(databases.Connections) != len(s.Database.Connections) || databases.Default != s.Database.Default {
		return Settings{}, fault.New(fault.Invalid, "database scopes must preserve all names and the default alias")
	}
	for name := range s.Database.Connections {
		if _, _, err := databases.ScopedSchema(name); err != nil {
			return Settings{}, err
		}
	}
	if _, _, err := databases.ScopedSchema(""); err != nil {
		return Settings{}, err
	}
	s.Cache.Stores = maps.Clone(s.Cache.Stores)
	for _, target := range cacheTargets(s) {
		name, schema, err := databases.ScopedSchema(target.Connection)
		if err != nil {
			return Settings{}, err
		}
		if target.Schema != "" && target.Schema != "public" && target.Schema != schema {
			return Settings{}, fault.New(fault.Invalid, "cache schema conflicts with database scope")
		}
		c := s.Cache.Stores[target.name]
		c.Database, c.Postgres.Schema = name, schema
		s.Cache.Stores[target.name] = c
	}
	s.Database = databases
	return s, nil
}
