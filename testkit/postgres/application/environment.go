// Package application binds retained PostgreSQL scopes to production application
// settings. It stays separate from the lightweight testkit/postgres helpers so
// low-level packages can use schema isolation without an infrastructure cycle.
package application

import (
	"fmt"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"maps"
	"slices"
)

// Selection explicitly states which named application connections share a scope.
// No original deployment endpoint or password survives binding.
type Selection struct {
	config postgres.Config
	names  []database.ConnectionName
	reads  bool
}

// On explicitly maps named application connections to a retained scope.
func On(scope *pgtest.Scope, names ...database.ConnectionName) Selection {
	return Selection{config: scope.Config(), names: slices.Clone(names)}
}

// WithReads enables a separate bounded read pool against the same opted-in test
// database/schema. By default all original read routing is explicitly disabled.
func (s Selection) WithReads() Selection { s.reads = true; return s }

// Environment contains scoped copies of every selected logical connection.
// It owns migration setup and registers bounded application cleanup through Start.
type Environment struct {
	settings infrastructure.DatabaseSettings
}

// Bind requires every configured connection exactly once. Separate Isolate calls
// express distinct namespaces; one On call expresses intentional sharing.
// Test pools use two connections per endpoint and retain the configured global
// ceiling. This neither mutates original settings nor reconfigures a live pool.
func Bind(settings infrastructure.DatabaseSettings, selections ...Selection) (*Environment, error) {
	if len(settings.Connections) == 0 || len(settings.Connections) > 128 || settings.MaxConnections <= 0 || settings.MaxConnections > 65536 {
		return nil, fault.New(fault.Invalid, "test scopes require bounded named database connections")
	}
	if _, ok := settings.Connections[settings.Default]; !ok {
		return nil, fault.New(fault.Missing, "test database default is not configured")
	}
	result := settings
	result.Connections = make(infrastructure.DatabaseConnections, len(settings.Connections))
	remaining := settings.MaxConnections
	for _, selected := range selections {
		if selected.config.Schema == "" || len(selected.names) == 0 {
			return nil, fault.New(fault.Invalid, "invalid PostgreSQL scope selection")
		}
		for _, name := range selected.names {
			if err := name.Validate(); err != nil {
				return nil, err
			}
			original, exists := settings.Connections[name]
			if !exists {
				return nil, fault.New(fault.Missing, "selected test database is not configured")
			}
			if _, exists := result.Connections[name]; exists {
				return nil, fault.New(fault.Duplicate, "test database scope selected twice")
			}
			scope := selected.config
			if original.Primary.Schema != "" && original.Primary.Schema != "public" && original.Primary.Schema != scope.Schema {
				return nil, fault.New(fault.Invalid, "primary schema conflicts with test scope")
			}
			if original.ReadEnabled && original.Read.Schema != "" && original.Read.Schema != "public" && original.Read.Schema != scope.Schema {
				return nil, fault.New(fault.Invalid, "read schema conflicts with test scope")
			}
			c := infrastructure.ConnectionSettings{Primary: infrastructure.PostgreSQLSettingsFromConfig(scope), Read: infrastructure.PostgreSQLSettingsFromConfig(scope), ReadEnabled: selected.reads, MaxConnections: scope.Pool.MaxOpen}
			if selected.reads {
				c.MaxConnections += scope.Pool.MaxOpen
			}
			remaining -= c.MaxConnections
			if remaining < 0 {
				return nil, fault.New(fault.Invalid, "test pools exceed application connection ceiling")
			}
			result.Connections[name] = c
		}
	}
	if len(result.Connections) != len(settings.Connections) {
		return nil, fault.New(fault.Missing, "every application database needs an explicit test scope")
	}
	return &Environment{settings: result}, nil
}

// Settings returns caller-owned settings; defaults still alias their named pool.
func (e *Environment) Settings() infrastructure.DatabaseSettings {
	if e == nil {
		return infrastructure.DatabaseSettings{}
	}
	result := e.settings
	result.Connections = maps.Clone(result.Connections)
	return result
}

func (Selection) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte("PostgreSQL test selection")) }
func (Environment) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("PostgreSQL test environment")) }
