package infrastructure_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
)

func migrationDatabases() infrastructure.DatabaseSettings {
	connection := func(database, schema string) infrastructure.ConnectionSettings {
		c := infrastructure.DefaultConnectionSettings()
		c.Primary.Host, c.Primary.Database, c.Primary.User, c.Primary.Schema = "db.internal", database, "app", schema
		return c
	}
	s := infrastructure.DefaultDatabaseSettings()
	s.Default = "main"
	// "alias" reaches main's database; "reports" is a scoped connection elsewhere.
	s.Connections = infrastructure.DatabaseConnections{"main": connection("app", ""), "alias": connection("app", ""), "reports": connection("reports", "analytics")}
	return s
}

func migrationDefinition(origin migrate.Origin, id migrate.ID, sql string) migrate.Definition {
	return migrate.Definition{Key: migrate.Key{Origin: origin, ID: id}, Version: "v1", SQL: []string{sql}}
}

func TestMigrationGroupsResolveMergeAndOrderTargets(t *testing.T) {
	shared := migrationDefinition("foundry.outbox", "000001_create", "CREATE TABLE foundry_outbox (id uuid PRIMARY KEY)")
	domain := migrationDefinition("app", "000001_records", "CREATE TABLE records (id uuid PRIMARY KEY)")
	targets := []infrastructure.MigrationTarget{
		{Definitions: []migrate.Definition{shared, domain}},
		{Connection: "alias", Schema: "public", Definitions: []migrate.Definition{shared}},
		{Connection: "main", Schema: "tenant", Definitions: []migrate.Definition{shared}},
		{Connection: "reports", Definitions: []migrate.Definition{migrationDefinition("app", "000001_totals", "CREATE TABLE totals (id bigint PRIMARY KEY)")}},
		{Connection: "main"},
	}
	groups, err := migrationDatabases().MigrationGroups(targets...)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		connections []database.ConnectionName
		schema      string
		definitions int
	}{{[]database.ConnectionName{"alias", "main"}, "public", 2}, {[]database.ConnectionName{"main"}, "tenant", 1}, {[]database.ConnectionName{"reports"}, "analytics", 1}}
	if len(groups) != len(want) {
		t.Fatalf("migration groups: %+v", groups)
	}
	for i, w := range want {
		if !slices.Equal(groups[i].Connections, w.connections) || groups[i].Schema != w.schema || len(groups[i].Definitions) != w.definitions {
			t.Fatalf("group %d: %+v", i, groups[i])
		}
	}
	targets[0].Definitions[0].SQL[0] = "changed after grouping"
	if groups[0].Definitions[0].SQL[0] != "CREATE TABLE foundry_outbox (id uuid PRIMARY KEY)" {
		t.Fatal("migration groups share caller definitions")
	}
	config := groups[2].PostgresConfig(migrate.DefaultPostgresConfig())
	if config.Schema != "analytics" || config.SearchPath != "analytics" || config.Table != migrate.DefaultPostgresConfig().Table || config.Validate() != nil {
		t.Fatalf("group history must live in its own schema: %+v", config)
	}
}

func TestMigrationGroupsRejectConflictsBeforeIO(t *testing.T) {
	definition := migrationDefinition("app", "000001_records", "CREATE TABLE records (id uuid PRIMARY KEY)")
	changed := definition
	changed.SQL = []string{"CREATE TABLE records (id bigint PRIMARY KEY)"}
	missing := definition
	missing.Requires = []migrate.Key{{Origin: "app", ID: "000000_absent"}}
	s := migrationDatabases()
	for name, test := range map[string]struct {
		targets []infrastructure.MigrationTarget
		kind    fault.Code
	}{
		"conflict in a shared history": {[]infrastructure.MigrationTarget{{Connection: "main", Definitions: []migrate.Definition{definition}}, {Connection: "alias", Definitions: []migrate.Definition{changed}}}, fault.Conflict},
		"unknown connection":           {[]infrastructure.MigrationTarget{{Connection: "unknown", Definitions: []migrate.Definition{definition}}}, fault.Missing},
		"invalid schema":               {[]infrastructure.MigrationTarget{{Schema: "tenant-a", Definitions: []migrate.Definition{definition}}}, fault.Invalid},
		"invalid registry":             {[]infrastructure.MigrationTarget{{Definitions: []migrate.Definition{missing}}}, fault.Missing},
	} {
		if _, err := s.MigrationGroups(test.targets...); !errors.Is(err, test.kind) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// One key in two schemas belongs to two separate histories.
	groups, err := s.MigrationGroups(infrastructure.MigrationTarget{Definitions: []migrate.Definition{definition}}, infrastructure.MigrationTarget{Schema: "tenant", Definitions: []migrate.Definition{changed}})
	if err != nil || len(groups) != 2 {
		t.Fatal("separate schemas were merged", err)
	}
}

func TestMigrationGroupsMergeAvailabilityWithoutChangingDefinitions(t *testing.T) {
	definition := migrationDefinition("foundry.shared", "001", "SELECT 1")
	for _, enabledFirst := range []bool{false, true} {
		first := migrate.FeatureDefinitions(enabledFirst, definition)
		second := migrate.FeatureDefinitions(!enabledFirst, definition)
		groups, err := migrationDatabases().MigrationGroups(
			infrastructure.MigrationTarget{Definitions: first},
			infrastructure.MigrationTarget{Connection: "alias", Definitions: second},
		)
		if err != nil || len(groups) != 1 || len(groups[0].Definitions) != 1 || groups[0].Definitions[0].Availability != migrate.Available {
			t.Fatal("shared enabled store lost", groups, err)
		}
		if first[0].Availability == second[0].Availability {
			t.Fatal("input availability mutated")
		}
		second[0].SQL[0] = "SELECT 2"
		if _, err := migrationDatabases().MigrationGroups(infrastructure.MigrationTarget{Definitions: first}, infrastructure.MigrationTarget{Definitions: second}); !errors.Is(err, fault.Conflict) {
			t.Fatal("availability hid definition drift", err)
		}
	}
}

func TestCacheMigrationAvailabilityIncludesEveryStoreSharingTheTarget(t *testing.T) {
	for _, activeName := range []string{"a", "b"} {
		s := infrastructure.DefaultSettings()
		s.Database = migrationDatabases()
		s.Cache.Default = "a"
		s.Cache.Stores = infrastructure.CacheStores{"a": infrastructure.DefaultCacheSettings(), "b": infrastructure.DefaultCacheSettings()}
		for name, store := range s.Cache.Stores {
			store.Database = "main"
			if string(name) == activeName {
				store.Driver = infrastructure.PostgresCache
			}
			s.Cache.Stores[name] = store
		}
		plan, err := infrastructure.Configure(s)
		if err != nil {
			t.Fatal(err)
		}
		targets := plan.Migrations()
		if len(targets) != 1 || len(targets[0].Definitions) == 0 {
			t.Fatal("shared cache targets not deduplicated", targets)
		}
		for _, definition := range targets[0].Definitions {
			if definition.Availability != migrate.Available {
				t.Fatal("inactive cache hid an active store", activeName)
			}
		}
	}
}

func TestMigrationGroupsRetainHistoryWhenRunnableDefinitionIsDeduplicated(t *testing.T) {
	active := migrationDefinition("app", "001", "SELECT 1")
	disabled := migrationDefinition("foundry.history", "001", "SELECT 2")
	disabled.Availability = migrate.FeatureDisabled
	registry, err := migrate.New(active, disabled)
	if err != nil {
		t.Fatal(err)
	}
	groups, err := migrationDatabases().MigrationGroups(
		infrastructure.MigrationTarget{Definitions: []migrate.Definition{active}},
		infrastructure.MigrationTarget{Definitions: registry.RunnableDefinitions()},
	)
	if err != nil || len(groups) != 1 || len(groups[0].Definitions) != 2 {
		t.Fatal("deduplication discarded disabled history", groups, err)
	}
}
