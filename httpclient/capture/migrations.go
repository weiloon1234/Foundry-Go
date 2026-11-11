package capture

import "github.com/weiloon1234/Foundry-Go/database/migrate"

const (
	// MigrationOrigin owns the framework's outbound capture table.
	MigrationOrigin migrate.Origin = "foundry.outbound_capture"
	// CreateTable creates foundry_outbound_captures.
	CreateTable migrate.ID = "000001_create_outbound_captures"
	// Introduced is the release that added the table.
	Introduced migrate.Version = "v0.19.0"
)

// Migrations returns the framework-owned capture table. Application assembly
// registers it whenever the application has a database, also while capture
// is disabled, so toggling capture never changes the migration set. Apply it
// with the ordinary migration command before capture can store rows.
func Migrations() []migrate.Definition {
	return []migrate.Definition{{Key: migrate.Key{Origin: MigrationOrigin, ID: CreateTable}, Version: Introduced, SQL: []string{
		`CREATE TABLE foundry_outbound_captures (
id uuid PRIMARY KEY,
operation_id uuid NOT NULL,
attempt smallint NOT NULL CHECK (attempt BETWEEN 0 AND 10),
retried boolean NOT NULL,
client text NOT NULL CHECK (octet_length(client) BETWEEN 1 AND 128),
started_at timestamptz NOT NULL CHECK (isfinite(started_at)),
headers_ms bigint CHECK (headers_ms IS NULL OR headers_ms >= 0),
duration_ms bigint NOT NULL CHECK (duration_ms >= 0),
result text NOT NULL CHECK (result IN ('completed','consumer_failed','response_body_failed','response_too_large','request_body_failed','transport_failed','timed_out','canceled','destination_denied','overloaded')),
request_id text CHECK (request_id IS NULL OR octet_length(request_id) BETWEEN 1 AND 128),
trace_id text CHECK (trace_id IS NULL OR trace_id ~ '^[0-9a-f]{32}$'),
route text CHECK (route IS NULL OR octet_length(route) BETWEEN 1 AND 256),
method text NOT NULL CHECK (octet_length(method) BETWEEN 1 AND 32),
scheme text NOT NULL CHECK (scheme IN ('http','https')),
host text NOT NULL CHECK (octet_length(host) BETWEEN 1 AND 300),
path text NOT NULL CHECK (octet_length(path) BETWEEN 1 AND 4096),
query jsonb NOT NULL CHECK (jsonb_typeof(query) = 'array' AND jsonb_array_length(query) <= 128),
request_headers jsonb NOT NULL CHECK (jsonb_typeof(request_headers) = 'array' AND jsonb_array_length(request_headers) <= 128),
body_mode text NOT NULL CHECK (body_mode IN ('redacted','full')),
request_body_state text NOT NULL CHECK (request_body_state IN ('empty','exact','exact_prefix','json','form','multipart','omitted')),
request_body_omission text CHECK (request_body_omission IN ('not_sent','no_response','incomplete','over_capture_limit','content_encoded','unsupported_media_type','malformed','redaction_limit','capture_memory','declined')),
request_body bytea NOT NULL CHECK (octet_length(request_body) <= 8388608),
request_declared_bytes bigint CHECK (request_declared_bytes IS NULL OR request_declared_bytes >= 0),
request_sent_bytes bigint NOT NULL CHECK (request_sent_bytes >= 0),
request_body_complete boolean NOT NULL,
status smallint CHECK (status IS NULL OR status BETWEEN 100 AND 599),
protocol text CHECK (protocol IS NULL OR octet_length(protocol) BETWEEN 1 AND 16),
remote_ip text CHECK (remote_ip IS NULL OR octet_length(remote_ip) BETWEEN 2 AND 64),
connection_reused boolean,
response_headers jsonb NOT NULL CHECK (jsonb_typeof(response_headers) = 'array' AND jsonb_array_length(response_headers) <= 128),
response_body_state text NOT NULL CHECK (response_body_state IN ('empty','exact','exact_prefix','json','form','multipart','omitted')),
response_body_omission text CHECK (response_body_omission IN ('not_sent','no_response','incomplete','over_capture_limit','content_encoded','unsupported_media_type','malformed','redaction_limit','capture_memory','declined')),
response_body bytea NOT NULL CHECK (octet_length(response_body) <= 8388608),
response_declared_bytes bigint CHECK (response_declared_bytes IS NULL OR response_declared_bytes >= 0),
response_read_bytes bigint NOT NULL CHECK (response_read_bytes >= 0),
response_body_complete boolean NOT NULL,
omissions jsonb NOT NULL CHECK (jsonb_typeof(omissions) = 'array' AND omissions <@ '["path_truncated","query_truncated","query_malformed","request_headers_truncated","response_headers_truncated"]'::jsonb),
dropped_before bigint NOT NULL CHECK (dropped_before >= 0),
CHECK ((request_body_state = 'omitted') = (request_body_omission IS NOT NULL)),
CHECK ((response_body_state = 'omitted') = (response_body_omission IS NOT NULL)),
CHECK ((request_body_state IN ('empty','omitted')) = (octet_length(request_body) = 0)),
CHECK ((response_body_state IN ('empty','omitted')) = (octet_length(response_body) = 0)),
CHECK (body_mode = 'full' OR (request_body_state NOT IN ('exact','exact_prefix') AND response_body_state NOT IN ('exact','exact_prefix'))),
CHECK (body_mode = 'redacted' OR (request_body_state IN ('empty','exact','exact_prefix','omitted') AND response_body_state IN ('empty','exact','exact_prefix','omitted'))),
CHECK (request_body_omission IS DISTINCT FROM 'no_response' AND response_body_omission IS DISTINCT FROM 'not_sent'),
CHECK ((status IS NULL) = (response_body_omission IS NOT DISTINCT FROM 'no_response')),
CHECK ((status IS NULL) = (headers_ms IS NULL)),
CHECK (result <> 'completed' OR status IS NOT NULL),
CHECK (result <> 'overloaded' OR attempt = 0),
CHECK (attempt > 0 OR (result IN ('overloaded','destination_denied') AND NOT retried AND request_body_omission IS NOT DISTINCT FROM 'not_sent')),
CHECK (attempt = 0 OR request_body_omission IS DISTINCT FROM 'not_sent')
)`,
		`CREATE INDEX foundry_outbound_captures_started ON foundry_outbound_captures (started_at DESC, id DESC)`,
		`CREATE INDEX foundry_outbound_captures_client ON foundry_outbound_captures (client, started_at DESC, id DESC)`,
		`CREATE INDEX foundry_outbound_captures_host ON foundry_outbound_captures (host, started_at DESC, id DESC)`,
		`CREATE INDEX foundry_outbound_captures_operation ON foundry_outbound_captures (operation_id)`,
		`CREATE INDEX foundry_outbound_captures_request ON foundry_outbound_captures (request_id) WHERE request_id IS NOT NULL`,
	}}}
}
