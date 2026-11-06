package telemetry

import "github.com/weiloon1234/Foundry-Go/database/migrate"

// Migrations returns owned, immutable history for a fresh telemetry store.
// Register once and apply explicitly with the ordinary migration runner in the
// same selected schema used by telemetry. This function performs no I/O and
// neither copies nor modifies application-owned developer telemetry tables.
func Migrations() []migrate.Definition {
	return []migrate.Definition{{Key: migrate.Key{Origin: "foundry.telemetry", ID: "000001_create_telemetry"}, Version: "v0.1.0", SQL: []string{
		`CREATE TABLE foundry_telemetry_controls (key text PRIMARY KEY CHECK (key = 'storage'))`,
		`INSERT INTO foundry_telemetry_controls (key) VALUES ('storage')`,
		`CREATE TABLE foundry_telemetry_runs (
id uuid PRIMARY KEY,
instance text NOT NULL CHECK (octet_length(instance) BETWEEN 1 AND 128),
release text NOT NULL CHECK (octet_length(release) BETWEEN 1 AND 128),
started_at timestamptz NOT NULL CHECK (isfinite(started_at)),
heartbeat_at timestamptz NOT NULL CHECK (isfinite(heartbeat_at)),
retain_until timestamptz NOT NULL CHECK (isfinite(retain_until)),
runtime jsonb NOT NULL CHECK (jsonb_typeof(runtime) = 'object' AND COALESCE(runtime->'version' = '1'::jsonb, false) AND octet_length(runtime::text) <= 2097152)
)`,
		`CREATE INDEX foundry_telemetry_runs_heartbeat ON foundry_telemetry_runs (heartbeat_at,id)`,
		`CREATE INDEX foundry_telemetry_runs_retention ON foundry_telemetry_runs (retain_until,id)`,
		`CREATE TABLE foundry_telemetry_flushes (
id uuid PRIMARY KEY,
run_id uuid NOT NULL REFERENCES foundry_telemetry_runs(id) ON DELETE CASCADE,
created_at timestamptz NOT NULL CHECK (isfinite(created_at)),
stored_at timestamptz NOT NULL DEFAULT clock_timestamp() CHECK (isfinite(stored_at)),
retain_until timestamptz NOT NULL CHECK (isfinite(retain_until) AND retain_until > stored_at),
result jsonb NOT NULL CHECK (jsonb_typeof(result) = 'object' AND COALESCE(result->'version' = '1'::jsonb, false) AND octet_length(result::text) <= 2097152)
)`,
		`CREATE INDEX foundry_telemetry_flushes_created ON foundry_telemetry_flushes (created_at,id)`,
		`CREATE INDEX foundry_telemetry_flushes_stored ON foundry_telemetry_flushes (stored_at,id)`,
		`CREATE INDEX foundry_telemetry_flushes_retention ON foundry_telemetry_flushes (retain_until,id)`,
		`CREATE INDEX foundry_telemetry_flushes_run ON foundry_telemetry_flushes (run_id,id)`,
		`CREATE TABLE foundry_telemetry_metric_buckets (
key text PRIMARY KEY CHECK (key ~ '^[0-9a-f]{64}$'),
run_id uuid NOT NULL REFERENCES foundry_telemetry_runs(id) ON DELETE CASCADE,
bucket_at timestamptz NOT NULL CHECK (isfinite(bucket_at)),
interval_seconds integer NOT NULL CHECK (interval_seconds IN (60,3600)),
route text NOT NULL, route_path text NOT NULL, method text NOT NULL,
status integer NOT NULL CHECK (status = 0 OR status BETWEEN 100 AND 599),
outcome text NOT NULL,
histogram jsonb NOT NULL CHECK (jsonb_typeof(histogram) = 'object' AND COALESCE(histogram->'version' = '1'::jsonb, false) AND octet_length(histogram::text) <= 16384)
)`,
		`CREATE INDEX foundry_telemetry_metrics_window ON foundry_telemetry_metric_buckets (interval_seconds,bucket_at,key)`,
		`CREATE INDEX foundry_telemetry_metrics_run ON foundry_telemetry_metric_buckets (run_id,key)`,
		`CREATE TABLE foundry_telemetry_issues (
fingerprint text PRIMARY KEY CHECK (fingerprint ~ '^[0-9a-f]{64}$'),
source text NOT NULL CHECK (source IN ('backend','browser','security')),
code text NOT NULL, route text NOT NULL, release text NOT NULL,
count bigint NOT NULL CHECK (count > 0),
first_at timestamptz NOT NULL CHECK (isfinite(first_at)),
last_at timestamptz NOT NULL CHECK (isfinite(last_at) AND last_at >= first_at),
detail jsonb NOT NULL CHECK (jsonb_typeof(detail) = 'object' AND COALESCE(detail->'version' = '1'::jsonb, false) AND octet_length(detail::text) <= 65536)
)`,
		`CREATE INDEX foundry_telemetry_issues_last ON foundry_telemetry_issues (source,last_at,fingerprint)`,
		`CREATE TABLE foundry_telemetry_events (
id uuid PRIMARY KEY,
fingerprint text NOT NULL CHECK (fingerprint ~ '^[0-9a-f]{64}$'),
source text NOT NULL CHECK (source IN ('backend','browser','security')),
created_at timestamptz NOT NULL CHECK (isfinite(created_at)),
detail jsonb NOT NULL CHECK (jsonb_typeof(detail) = 'object' AND COALESCE(detail->'version' = '1'::jsonb, false) AND octet_length(detail::text) <= 65536)
)`,
		`CREATE INDEX foundry_telemetry_events_created ON foundry_telemetry_events (source,created_at,id)`,
		`CREATE INDEX foundry_telemetry_events_issue ON foundry_telemetry_events (fingerprint,created_at,id)`,
		`CREATE TABLE foundry_telemetry_alerts (
key text PRIMARY KEY CHECK (key ~ '^[0-9a-f]{64}$'),
run_id uuid NOT NULL REFERENCES foundry_telemetry_runs(id) ON DELETE CASCADE,
rule text NOT NULL CHECK (rule IN ('latency','http_errors','pool_pressure','log_failures','telemetry_loss','authentication_burst')),
state text NOT NULL CHECK (state IN ('open','acknowledged','recovered')),
episode bigint NOT NULL CHECK (episode > 0),
first_at timestamptz NOT NULL CHECK (isfinite(first_at)),
last_at timestamptz NOT NULL CHECK (isfinite(last_at) AND last_at >= first_at),
recovered_at timestamptz NULL CHECK (recovered_at IS NULL OR isfinite(recovered_at)),
UNIQUE (run_id,rule)
)`,
		`CREATE INDEX foundry_telemetry_alerts_state ON foundry_telemetry_alerts (state,last_at,key)`,
	}}}
}
