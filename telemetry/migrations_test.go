package telemetry

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/telemetry/internal/store"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

// Derived from migrate.Registry's format-1 canonical definition encoding.
// This pins the fresh-store migration's history; later changes need a new ID.
const telemetryMigrationChecksum = "80da6e0be4b645ca8dcdbc00c000535fdcc4396905bf21a9c96afb92ca2d0526"

func TestTelemetryMigrationIdentityAndOwnership(t *testing.T) {
	definitions := Migrations()
	if len(definitions) != 1 {
		t.Fatal("unexpected fresh-store migration count")
	}
	wantKey := migrate.Key{Origin: "foundry.telemetry", ID: "000001_create_telemetry"}
	if definitions[0].Key != wantKey || definitions[0].Version != "v0.1.0" || definitions[0].Mode != migrate.Transactional || len(definitions[0].Requires) != 0 || len(definitions[0].Down) != 0 {
		t.Fatal("fresh-store migration identity, dependency or reversal policy changed")
	}
	registry, err := migrate.New(definitions...)
	if err != nil {
		t.Fatal(err)
	}
	entry := registry.Entries()[0]
	if entry.Checksum.String() != telemetryMigrationChecksum {
		t.Fatalf("historical telemetry migration changed: %s", entry.Checksum.String())
	}
	baseline := definitions[0].Clone()
	definitions[0].SQL[0] = "SELECT 1"
	definitions[0].Version = "modified"
	if !reflect.DeepEqual(Migrations()[0], baseline) {
		t.Fatal("Migrations returns shared mutable definition storage")
	}
	if current := registry.Entries()[0]; !reflect.DeepEqual(current, entry) {
		t.Fatal("registered migration changed after caller mutation")
	}
	changed := Migrations()
	changed[0].SQL = append(changed[0].SQL, "SELECT 1")
	changedRegistry, err := migrate.New(changed...)
	if err != nil {
		t.Fatal(err)
	}
	history := []migrate.Applied{{Key: entry.Key, Version: entry.Version, Checksum: entry.Checksum, Batch: 1, AppliedAt: time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)}}
	report, err := changedRegistry.Inspect(history)
	if err != nil || !errors.Is(report.Check(), fault.Conflict) {
		t.Fatal("migration history accepted changed SQL", err)
	}
	if err := orm.Check(storageTables()...); err != nil {
		t.Fatal("telemetry storage declarations are invalid", err)
	}
}

func telemetryMigrationFixture(t *testing.T) (*database.DB, *migrate.Postgres, string) {
	t.Helper()
	scope := pgtest.Isolate(t)
	db := scope.Open(t)
	return db, telemetryMigrationRunner(t, db, scope.Schema()), scope.Schema()
}

func telemetryMigrationRunner(t *testing.T, db *database.DB, schema string) *migrate.Postgres {
	t.Helper()
	registry, err := migrate.New(Migrations()...)
	if err != nil {
		t.Fatal(err)
	}
	config := migrate.DefaultPostgresConfig()
	config.Schema, config.SearchPath = schema, schema
	runner, err := migrate.NewPostgres(db, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func TestPostgresTelemetryFreshMigrationAndHistory(t *testing.T) {
	db, runner, schema := telemetryMigrationFixture(t)
	status, err := runner.Status(t.Context())
	if err != nil || len(status.Statuses) != 1 || status.Statuses[0].State != migrate.Pending {
		t.Fatal("fresh store did not start pending", err)
	}
	applied, err := runner.Up(t.Context())
	if err != nil || len(applied.Applied) != 1 || applied.Applied[0].Checksum.String() != telemetryMigrationChecksum {
		t.Fatal("fresh migration failed or stored a different checksum", err)
	}
	again, err := runner.Up(t.Context())
	if err != nil || len(again.Applied) != 0 {
		t.Fatal("repeated migration reapplied telemetry", err)
	}
	status, err = runner.Status(t.Context())
	if err != nil || status.Check() != nil || len(status.Statuses) != 1 || status.Statuses[0].State != migrate.Complete {
		t.Fatal("installed history is not complete", err)
	}
	var tables string
	if err := database.ScanOne(t.Context(), db.Primary(), `SELECT string_agg(tablename,',' ORDER BY tablename) FROM pg_catalog.pg_tables WHERE schemaname=$1 AND tablename LIKE 'foundry_telemetry_%'`, []any{schema}, &tables); err != nil {
		t.Fatal(err)
	}
	if tables != "foundry_telemetry_alerts,foundry_telemetry_controls,foundry_telemetry_events,foundry_telemetry_flushes,foundry_telemetry_issues,foundry_telemetry_metric_buckets,foundry_telemetry_runs" {
		t.Fatal("fresh telemetry table inventory changed", tables)
	}
	if counts := countTelemetry(t, db, schema); counts != (telemetryCounts{}) {
		t.Fatal("fresh telemetry did not start empty", counts)
	}
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		count, err := orm.Count(t.Context(), tx, alerts.Query())
		if err != nil {
			return err
		}
		if count != 0 {
			t.Error("fresh telemetry alerts did not start empty")
		}
		_, err = orm.RequireKey(t.Context(), tx, store.Controls.Table, "storage")
		return err
	})
	assertTelemetrySchema(t, db, schema)
	changed := Migrations()
	changed[0].SQL = append(changed[0].SQL, "SELECT 1")
	registry, err := migrate.New(changed...)
	if err != nil {
		t.Fatal(err)
	}
	config := migrate.DefaultPostgresConfig()
	config.Schema, config.SearchPath = schema, schema
	drifted, err := migrate.NewPostgres(db, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := drifted.Up(t.Context()); !errors.Is(err, fault.Conflict) {
		t.Fatal("runner rewrote installed telemetry history")
	}
}

func assertTelemetrySchema(t *testing.T, db *database.DB, schema string) {
	t.Helper()
	var primary, cascades, actorColumns int64
	if err := database.ScanOne(t.Context(), db.Primary(), `SELECT count(*) FILTER (WHERE con.contype='p'),count(*) FILTER (WHERE con.contype='f' AND con.confdeltype='c') FROM pg_catalog.pg_constraint con JOIN pg_catalog.pg_class rel ON rel.oid=con.conrelid JOIN pg_catalog.pg_namespace ns ON ns.oid=rel.relnamespace WHERE ns.nspname=$1 AND rel.relname LIKE 'foundry_telemetry_%'`, []any{schema}, &primary, &cascades); err != nil || primary != 7 || cascades != 3 {
		t.Fatal("typed primary keys or run-owned cascades changed", primary, cascades, err)
	}
	if err := database.ScanOne(t.Context(), db.Primary(), `SELECT count(*) FROM information_schema.columns WHERE table_schema=$1 AND table_name='foundry_telemetry_alerts' AND column_name IN ('actor_id','acknowledged_by')`, []any{schema}, &actorColumns); err != nil || actorColumns != 0 {
		t.Fatal("framework incidents acquired an app actor column", err)
	}
	var receiptDefault string
	if err := database.ScanOne(t.Context(), db.Primary(), `SELECT column_default FROM information_schema.columns WHERE table_schema=$1 AND table_name='foundry_telemetry_flushes' AND column_name='stored_at'`, []any{schema}, &receiptDefault); err != nil || !strings.Contains(receiptDefault, "clock_timestamp()") {
		t.Fatal("receipt storage time no longer uses the PostgreSQL clock", err)
	}
	want := map[string]string{
		"foundry_telemetry_runs_heartbeat":    "(heartbeat_at, id)",
		"foundry_telemetry_runs_retention":    "(retain_until, id)",
		"foundry_telemetry_flushes_created":   "(created_at, id)",
		"foundry_telemetry_flushes_stored":    "(stored_at, id)",
		"foundry_telemetry_flushes_retention": "(retain_until, id)",
		"foundry_telemetry_flushes_run":       "(run_id, id)",
		"foundry_telemetry_metrics_window":    "(interval_seconds, bucket_at, key)",
		"foundry_telemetry_metrics_run":       "(run_id, key)",
		"foundry_telemetry_issues_last":       "(source, last_at, fingerprint)",
		"foundry_telemetry_events_created":    "(source, created_at, id)",
		"foundry_telemetry_events_issue":      "(fingerprint, created_at, id)",
		"foundry_telemetry_alerts_state":      "(state, last_at, key)",
	}
	stream, err := db.Primary().Query(t.Context(), `SELECT indexname,indexdef FROM pg_catalog.pg_indexes WHERE schemaname=$1 AND tablename LIKE 'foundry_telemetry_%'`, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	for stream.Next() {
		var name, definition string
		if err := stream.Scan(&name, &definition); err != nil {
			t.Fatal(err)
		}
		if columns, ok := want[name]; ok {
			if !strings.HasSuffix(definition, columns) {
				t.Errorf("index %s lost its query/order columns", name)
			}
			delete(want, name)
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	for name := range want {
		t.Errorf("missing telemetry index %s", name)
	}
}

func TestPostgresTelemetryMigrationRejectsInvalidPayloadVersions(t *testing.T) {
	db, runner, schema := telemetryMigrationFixture(t)
	if _, err := runner.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		id, err := model.ParseID[runRow]("00000000-0000-7000-8000-000000000002")
		if err != nil {
			return err
		}
		now, err := serverTime(t.Context(), tx)
		if err != nil {
			return err
		}
		until, err := now.Add(24 * time.Hour)
		if err != nil {
			return err
		}
		payload, err := encodePayload(Runtime{}, maxRuntimePayloadBytes)
		if err != nil {
			return err
		}
		_, err = orm.Insert(t.Context(), tx, runs.Table, orm.KeySet(runs.Key(), id), orm.TextSet(runs.Instance, Instance("test")), orm.TextSet(runs.Release, Release("test")), orm.TimeSet(runs.StartedAt, now), orm.TimeSet(runs.HeartbeatAt, now), orm.TimeSet(runs.RetainUntil, until), orm.JSONSet(runs.Runtime, payload))
		return err
	})
	// Direct fixtures bypass typed JSON decoding to exercise SQL CHECK semantics.
	for _, payload := range []struct {
		name, insert string
		limit        int
	}{
		{"runtime", `INSERT INTO foundry_telemetry_runs (id,instance,release,started_at,heartbeat_at,retain_until,runtime) VALUES ('00000000-0000-7000-8000-000000000001','test','test',clock_timestamp(),clock_timestamp(),clock_timestamp()+interval '1 day',$1::jsonb)`, maxRuntimePayloadBytes},
		{"receipt", `INSERT INTO foundry_telemetry_flushes (id,run_id,created_at,retain_until,result) VALUES ('00000000-0000-7000-8000-000000000001','00000000-0000-7000-8000-000000000002',clock_timestamp(),clock_timestamp()+interval '1 day',$1::jsonb)`, maxReceiptPayloadBytes},
		{"histogram", `INSERT INTO foundry_telemetry_metric_buckets (key,run_id,bucket_at,interval_seconds,route,route_path,method,status,outcome,histogram) VALUES (repeat('a',64),'00000000-0000-7000-8000-000000000002',clock_timestamp(),60,'route','/','GET',200,'success',$1::jsonb)`, maxHistogramPayloadBytes},
		{"issue detail", `INSERT INTO foundry_telemetry_issues (fingerprint,source,code,route,release,count,first_at,last_at,detail) VALUES (repeat('a',64),'backend','code','route','release',1,now(),now(),$1::jsonb)`, maxDetailPayloadBytes},
		{"event detail", `INSERT INTO foundry_telemetry_events (id,fingerprint,source,created_at,detail) VALUES ('00000000-0000-7000-8000-000000000001',repeat('a',64),'backend',clock_timestamp(),$1::jsonb)`, maxDetailPayloadBytes},
	} {
		t.Run(payload.name, func(t *testing.T) {
			for _, document := range []struct{ name, text string }{
				{"missing", `{}`}, {"null", `{"version":null}`}, {"string", `{"version":"1"}`},
				{"zero", `{"version":0}`}, {"future", `{"version":2}`}, {"array", `[]`},
				{"oversize", `{"version":1,"data":"` + strings.Repeat("x", payload.limit) + `"}`},
			} {
				t.Run(document.name, func(t *testing.T) {
					if _, err := db.Exec(t.Context(), payload.insert, document.text); !errors.Is(err, database.CheckViolation) {
						t.Fatal("database accepted an unsupported or oversized payload", err)
					}
				})
			}
		})
	}
}
