package application

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/audit"
	mfapg "github.com/weiloon1234/Foundry-Go/auth/mfa/postgres"
	sessionpg "github.com/weiloon1234/Foundry-Go/auth/session/postgres"
	tokenpg "github.com/weiloon1234/Foundry-Go/auth/token/postgres"
	cachepg "github.com/weiloon1234/Foundry-Go/cache/postgres"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
	outboundcapture "github.com/weiloon1234/Foundry-Go/httpclient/capture"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/jobs/archive"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/notifications"
	"github.com/weiloon1234/Foundry-Go/outbox"
	storedsettings "github.com/weiloon1234/Foundry-Go/settings"
	"github.com/weiloon1234/Foundry-Go/telemetry"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/translations"
	"github.com/weiloon1234/Foundry-Go/webhook/capture"
)

type migrationFeatureCase struct {
	name        string
	toggle      func(*Settings, bool)
	definitions func() []migrate.Definition
}

func migrationFeatures() []migrationFeatureCase {
	return []migrationFeatureCase{
		{"sessions", func(s *Settings, on bool) { s.Features.Auth.Sessions.Enabled = on }, sessionpg.Migrations},
		{"tokens", func(s *Settings, on bool) { s.Features.Auth.Tokens.Enabled = on }, tokenpg.Migrations},
		{"mfa", func(s *Settings, on bool) { s.Features.Auth.MFA.Enabled = on }, mfapg.Migrations},
		{"idempotency", func(s *Settings, on bool) { s.Features.Idempotency.Enabled = on }, idempotency.Migrations},
		{"outbox", func(s *Settings, on bool) { s.Features.Outbox.Enabled = on }, outbox.Migrations},
		{"audit", func(s *Settings, on bool) { s.Features.Audit.Enabled = on }, audit.Migrations},
		{"telemetry", func(s *Settings, on bool) { s.Features.Telemetry.Enabled = on }, telemetry.Migrations},
		{"notifications", func(s *Settings, on bool) { s.Features.Notifications.Enabled = on }, notifications.Migrations},
		{"webhook_capture", func(s *Settings, on bool) { s.Features.WebhookCapture.declared = on }, capture.Migrations},
		{"outbound_capture_store", func(s *Settings, on bool) { s.Features.OutboundCapture.present = on }, outboundcapture.Migrations},
		{"extensions", func(s *Settings, on bool) { s.Features.Extensions.Enabled = on }, func() []migrate.Definition { return append(storedsettings.Migrations(), metadata.Migrations()...) }},
		{"translations", func(s *Settings, on bool) { s.Features.Extensions.Enabled = true; s.Features.Locales.Enabled = on }, translations.Migrations},
		{"attachments", func(s *Settings, on bool) { s.Features.Extensions.Enabled = true; s.Features.Attachments.Enabled = on }, attachments.Migrations},
		{"extensions_parent", func(s *Settings, on bool) {
			s.Features.Extensions.Enabled = on
			s.Features.Locales.Enabled = true
			s.Features.Attachments.Enabled = true
		}, func() []migrate.Definition {
			definitions := append(storedsettings.Migrations(), metadata.Migrations()...)
			definitions = append(definitions, translations.Migrations()...)
			return append(definitions, attachments.Migrations()...)
		}},
		{"archive", func(s *Settings, on bool) { s.Worker.Archive.Enabled = on }, archive.Migrations},
		{"postgres_cache", func(s *Settings, on bool) {
			store := s.Services.Cache.Stores[s.Services.Cache.Default]
			store.Driver = infrastructure.MemoryCache
			if on {
				store.Driver = infrastructure.PostgresCache
			}
			s.Services.Cache.Stores[s.Services.Cache.Default] = store
		}, cachepg.Migrations},
	}
}

func featureMigrationSettings(schema string) Settings {
	s := DefaultSettings()
	connection := infrastructure.DefaultConnectionSettings()
	connection.Primary.Host, connection.Primary.Database, connection.Primary.User, connection.Primary.Schema = "db.internal", "app", "app", schema
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": connection}
	for _, target := range persistenceTargets(&s) {
		*target.connection, *target.schema = "default", schema
	}
	s.Worker.Archive.Database, s.Worker.Archive.Schema = "default", schema
	store := infrastructure.DefaultCacheSettings()
	s.Services.Cache.Stores = make(infrastructure.CacheStores)
	store.Database, store.Postgres.Schema = "default", schema
	s.Services.Cache.Stores[s.Services.Cache.Default] = store
	return s
}

func collectedFeatureRegistry(t *testing.T, s Settings, extra ...migrate.Definition) *migrate.Registry {
	t.Helper()
	plan, err := infrastructure.Configure(s.Services)
	if err != nil {
		t.Fatal(err)
	}
	groups, err := featureMigrations(plan, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err != nil || len(groups) != 1 {
		t.Fatalf("feature groups: %+v %v", groups, err)
	}
	registry, err := migrate.New(append(groups[0].Definitions, extra...)...)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestEveryConditionalFeatureRetainsMigrationHistory(t *testing.T) {
	for _, feature := range migrationFeatures() {
		t.Run(feature.name, func(t *testing.T) {
			s := featureMigrationSettings("feature_history")
			feature.toggle(&s, true)
			enabled := collectedFeatureRegistry(t, s)
			wanted := feature.definitions()
			var history []migrate.Applied
			for _, definition := range wanted {
				found := slices.IndexFunc(enabled.Entries(), func(e migrate.Entry) bool { return e.Key == definition.Key })
				if found < 0 {
					t.Fatal("enabled definition absent", definition.Key)
				}
				entry := enabled.Entries()[found]
				if entry.Availability != migrate.Available {
					t.Fatal("enabled feature unavailable", entry)
				}
				history = append(history, migrate.Applied{Key: entry.Key, Version: entry.Version, Checksum: entry.Checksum, Batch: 1, AppliedAt: time.Now()})
			}
			feature.toggle(&s, false)
			disabled := collectedFeatureRegistry(t, s)
			report, err := disabled.Inspect(history)
			if err != nil || report.Check() != nil {
				t.Fatalf("disabled history: %+v %v", report, err)
			}
			empty, err := disabled.Inspect(nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, definition := range wanted {
				index := slices.IndexFunc(report.Statuses, func(status migrate.Status) bool { return status.Key == definition.Key })
				if index < 0 || report.Statuses[index].State != migrate.Complete || report.Statuses[index].Definition.Availability != migrate.FeatureDisabled || empty.Statuses[index].State != migrate.PendingDisabled {
					t.Fatal("feature history/availability lost", definition.Key)
				}
			}
			feature.toggle(&s, true)
			pending, err := collectedFeatureRegistry(t, s).Pending(nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, definition := range wanted {
				if !slices.ContainsFunc(pending, func(e migrate.Entry) bool { return e.Key == definition.Key }) {
					t.Fatal("re-enabled definition absent", definition.Key)
				}
			}
		})
	}
}

func TestConditionalFeatureMigrationsPostgres(t *testing.T) {
	for _, feature := range migrationFeatures() {
		t.Run(feature.name, func(t *testing.T) {
			db := pgtest.Open(t)
			schema := pgtest.Namespace(t, db)
			s := featureMigrationSettings(schema)
			config := migrate.DefaultPostgresConfig()
			config.Schema, config.SearchPath = schema, schema
			runner := func(registry *migrate.Registry) *migrate.Postgres {
				t.Helper()
				result, err := migrate.NewPostgres(db, registry, config)
				if err != nil {
					t.Fatal(err)
				}
				return result
			}
			feature.toggle(&s, true)
			initial, err := runner(collectedFeatureRegistry(t, s)).Up(t.Context())
			if err != nil || len(initial.Applied) == 0 {
				t.Fatal("initial feature migration", initial, err)
			}
			// Simulate the next framework release while this feature is disabled.
			later := migrate.Definition{Key: migrate.Key{Origin: feature.definitions()[0].Key.Origin, ID: "999999_test_pending"}, Version: "vnext", SQL: []string{"CREATE TABLE feature_later (id bigint PRIMARY KEY)"}, Down: []string{"DROP TABLE feature_later"}, Requires: []migrate.Key{feature.definitions()[0].Key}}
			unrelated := migrate.Definition{Key: migrate.Key{Origin: "app", ID: "999998_unrelated"}, Version: "v1", SQL: []string{"CREATE TABLE unrelated (id bigint PRIMARY KEY)", "INSERT INTO unrelated VALUES (7)"}, Down: []string{"DROP TABLE unrelated"}}
			feature.toggle(&s, false)
			disabledLater := migrate.FeatureDefinitions(false, later)[0]
			off := runner(collectedFeatureRegistry(t, s, disabledLater, unrelated))
			report, err := off.Status(t.Context())
			if err != nil || report.Check() != nil {
				t.Fatalf("status after disable: %+v %v", report, err)
			}
			index := slices.IndexFunc(report.Statuses, func(status migrate.Status) bool { return status.Key == later.Key })
			if index < 0 || report.Statuses[index].State != migrate.PendingDisabled {
				t.Fatal("disabled pending status absent")
			}
			applied, err := off.Up(t.Context())
			if err != nil || len(applied.Applied) != 1 || applied.Applied[0].Key != unrelated.Key {
				t.Fatal("unrelated migration blocked", applied, err)
			}
			var rows int64
			if err := database.ScanOne(t.Context(), db, `SELECT count(*) FROM "`+schema+`".unrelated WHERE id = 7`, nil, &rows); err != nil || rows != 1 {
				t.Fatal("unrelated SQL did not persist", rows, err)
			}
			if err := database.ScanOne(t.Context(), db, "SELECT count(*) FROM pg_catalog.pg_tables WHERE schemaname=$1 AND tablename='feature_later'", []any{schema}, &rows); err != nil || rows != 0 {
				t.Fatal("disabled SQL executed", rows, err)
			}
			feature.toggle(&s, true)
			on := runner(collectedFeatureRegistry(t, s, later, unrelated))
			applied, err = on.Up(t.Context())
			if err != nil || len(applied.Applied) != 1 || applied.Applied[0].Key != later.Key {
				t.Fatal("re-enabled pending migration not applied", applied, err)
			}
			if err := database.ScanOne(t.Context(), db, "SELECT count(*) FROM pg_catalog.pg_tables WHERE schemaname=$1 AND tablename='feature_later'", []any{schema}, &rows); err != nil || rows != 1 {
				t.Fatal("re-enabled SQL did not persist", rows, err)
			}
			// Disabling again must refuse the newest rollback, not skip it.
			feature.toggle(&s, false)
			off = runner(collectedFeatureRegistry(t, s, disabledLater, unrelated))
			if _, err := off.RollbackPlan(t.Context(), 1); !errors.Is(err, fault.Conflict) {
				t.Fatal("disabled rollback plan accepted", err)
			}
			result, err := off.Rollback(t.Context(), 1)
			if !errors.Is(err, fault.Conflict) || len(result.RolledBack) != 0 {
				t.Fatal("disabled rollback executed", result, err)
			}
			report, err = off.Status(t.Context())
			if err != nil || report.Check() != nil {
				t.Fatal("rollback changed history", report, err)
			}
			index = slices.IndexFunc(report.Statuses, func(status migrate.Status) bool { return status.Key == later.Key })
			if index < 0 || report.Statuses[index].State != migrate.Complete {
				t.Fatal("disabled applied history was removed")
			}
		})
	}
}

func TestArchiveHistoryTargetDefaultsDoNotDependOnEnablement(t *testing.T) {
	s := featureMigrationSettings("scoped_database")
	s.Worker.Archive = JobArchiveSettings{Enabled: true}
	enabled, err := prepareJobArchive(s)
	if err != nil {
		t.Fatal(err)
	}
	s.Worker.Archive.Enabled = false
	disabled, err := prepareJobArchive(s)
	if err != nil || enabled.Database != disabled.Database || enabled.Schema != disabled.Schema || disabled.Schema != "public" {
		t.Fatal("archive target moved when disabled", enabled, disabled, err)
	}
}
