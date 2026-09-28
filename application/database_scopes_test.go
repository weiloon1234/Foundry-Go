package application

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"testing"
)

func TestDatabaseScopesAlignEveryPersistenceDeclaration(t *testing.T) {
	s := DefaultSettings()
	c := infrastructure.DefaultConnectionSettings()
	c.Primary.Host, c.Primary.Database, c.Primary.User, c.Primary.Schema = "127.0.0.1", "test", "test", "isolated"
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": c, "audit": c}
	s.Features.Auth.Sessions.Enabled, s.Features.Auth.Tokens.Enabled, s.Features.Outbox.Enabled, s.Features.Audit.Enabled = true, true, true, true
	s.Features.Extensions.Enabled, s.Features.Notifications.Enabled, s.Features.Reports.Enabled = true, true, true
	s.Features.Audit.Database = "audit"
	s.Features.Idempotency.Enabled = true
	s.Features.Idempotency.Database = "audit"
	cache := infrastructure.DefaultCacheSettings()
	cache.Driver = infrastructure.PostgresCache
	s.Services.Cache.Stores = infrastructure.CacheStores{"default": cache}
	result, err := s.WithDatabaseScopes(s.Services.Database)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range persistenceTargets(&result.Features) {
		if *target.schema != "isolated" || *target.connection == "" {
			t.Fatal("persistence target did not follow scope")
		}
	}
	if result.Features.Audit.Database != "audit" || result.Services.Cache.Stores["default"].Postgres.Schema != "isolated" {
		t.Fatal("named store/migration scope changed")
	}
	if s.Features.Audit.Schema != "public" || s.Services.Cache.Stores["default"].Postgres.Schema != "public" {
		t.Fatal("original feature settings mutated")
	}
	for _, target := range persistenceTargets(&s.Features) {
		previous := *target.schema
		*target.schema = "explicit_other"
		if _, err := s.WithDatabaseScopes(s.Services.Database); !errors.Is(err, fault.Invalid) {
			t.Fatal("explicit feature override accepted", err)
		}
		*target.schema = previous
	}
	c.ReadEnabled = true
	c.Read = c.Primary
	c.Read.Schema = "other"
	changed := infrastructure.DatabaseSettings{Default: "default", MaxConnections: 128, Connections: infrastructure.DatabaseConnections{"default": c, "audit": c}}
	if _, err := s.WithDatabaseScopes(changed); !errors.Is(err, fault.Invalid) {
		t.Fatal("unscoped read endpoint accepted")
	}
	for _, name := range []database.ConnectionName{"absent", ""} {
		bad := changed
		bad.Default = name
		if _, err := s.WithDatabaseScopes(bad); err == nil {
			t.Fatal("default alias changed")
		}
	}
}
