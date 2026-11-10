package capture

import "github.com/weiloon1234/Foundry-Go/database/migrate"

const (
	MigrationOrigin migrate.Origin  = "foundry.webhook_capture"
	CreateTable     migrate.ID      = "000001_create_webhook_captures"
	Introduced      migrate.Version = "v0.17.0"
)

// Migrations returns the framework-owned capture table. Application assembly
// registers it for the configured database and schema; apply it with the
// ordinary migration command before capture can store rows. Rolling back the
// framework keeps the table.
func Migrations() []migrate.Definition {
	return []migrate.Definition{{Key: migrate.Key{Origin: MigrationOrigin, ID: CreateTable}, Version: Introduced, SQL: []string{
		`CREATE TABLE foundry_webhook_captures (
id uuid PRIMARY KEY,
received_at timestamptz NOT NULL CHECK (isfinite(received_at)),
method text NOT NULL CHECK (octet_length(method) BETWEEN 1 AND 32),
path text NOT NULL CHECK (octet_length(path) BETWEEN 1 AND 4096),
route text CHECK (route IS NULL OR octet_length(route) BETWEEN 1 AND 256),
client_ip text CHECK (client_ip IS NULL OR octet_length(client_ip) BETWEEN 2 AND 64),
headers jsonb NOT NULL CHECK (jsonb_typeof(headers) = 'array' AND jsonb_array_length(headers) <= 128),
query jsonb NOT NULL CHECK (jsonb_typeof(query) = 'array' AND jsonb_array_length(query) <= 128),
content_type text NOT NULL CHECK (octet_length(content_type) <= 256),
content_encoding text NOT NULL CHECK (octet_length(content_encoding) <= 256),
declared_bytes bigint CHECK (declared_bytes IS NULL OR declared_bytes >= 0),
measured_bytes bigint CHECK (measured_bytes IS NULL OR measured_bytes >= 0),
observed_bytes bigint NOT NULL CHECK (observed_bytes >= 0),
body_mode text NOT NULL CHECK (body_mode IN ('redacted','full')),
body_state text NOT NULL CHECK (body_state IN ('empty','exact','json','form','multipart','omitted')),
body_omission text CHECK (body_omission IN ('unread','over_capture_limit','body_limit','client_disconnect','read_timeout','read_failed','content_encoded','unsupported_media_type','malformed','redaction_limit')),
body bytea NOT NULL CHECK (octet_length(body) <= 8388608),
response_status integer CHECK (response_status IS NULL OR response_status BETWEEN 100 AND 999),
outcome text NOT NULL CHECK (outcome IN ('succeeded','failed','rejected','cancelled','timed_out','panicked')),
response_state text NOT NULL CHECK (response_state IN ('complete','write_failed','aborted','hijacked')),
response_content_type text NOT NULL CHECK (octet_length(response_content_type) <= 256),
response_encoding text NOT NULL CHECK (octet_length(response_encoding) <= 256),
response_bytes bigint NOT NULL CHECK (response_bytes >= 0),
response_head bytea NOT NULL CHECK (octet_length(response_head) <= 4096),
duration_ms bigint NOT NULL CHECK (duration_ms >= 0),
omissions jsonb NOT NULL CHECK (jsonb_typeof(omissions) = 'array' AND omissions <@ '["method_truncated","path_truncated","query_truncated","query_malformed","headers_truncated","content_type_truncated","content_encoding_truncated","response_truncated","response_metadata_truncated"]'::jsonb),
dropped_before bigint NOT NULL CHECK (dropped_before >= 0),
CHECK ((body_state = 'omitted') = (body_omission IS NOT NULL)),
CHECK (body_state IN ('empty','omitted') OR octet_length(body) > 0),
CHECK (body_state NOT IN ('empty','omitted') OR octet_length(body) = 0),
CHECK (body_state <> 'exact' OR body_mode = 'full'),
CHECK (body_mode = 'redacted' OR body_state IN ('empty','exact','omitted'))
)`,
		`CREATE INDEX foundry_webhook_captures_received ON foundry_webhook_captures (received_at DESC, id DESC)`,
		`CREATE INDEX foundry_webhook_captures_method ON foundry_webhook_captures (method, received_at DESC, id DESC)`,
		`CREATE INDEX foundry_webhook_captures_status ON foundry_webhook_captures (response_status, received_at DESC, id DESC)`,
	}}}
}
