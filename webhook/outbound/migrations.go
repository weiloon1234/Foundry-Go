package outbound

import "github.com/weiloon1234/Foundry-Go/database/migrate"

const (
	MigrationOrigin migrate.Origin  = "foundry.webhooks.outbound"
	CreateTables    migrate.ID      = "000001_create_outbound_webhooks"
	Introduced      migrate.Version = "v0.1.0"
)

// Migrations returns the owned endpoint, secret and delivery-log tables. Run
// them in the same schema as the job outbox that delivery jobs are enqueued to.
func Migrations() []migrate.Definition {
	return []migrate.Definition{{Key: migrate.Key{Origin: MigrationOrigin, ID: CreateTables}, Version: Introduced, SQL: []string{
		`CREATE TABLE foundry_webhook_endpoints (
id uuid PRIMARY KEY,
url text NOT NULL CHECK (octet_length(url) BETWEEN 1 AND 2048),
events jsonb NOT NULL CHECK (jsonb_typeof(events)='array' AND jsonb_array_length(events)<=64),
active boolean NOT NULL,
created_at timestamptz NOT NULL CHECK (isfinite(created_at)),
updated_at timestamptz NOT NULL CHECK (isfinite(updated_at))
)`,
		`CREATE TABLE foundry_webhook_secrets (
id uuid PRIMARY KEY,
endpoint_id uuid NOT NULL REFERENCES foundry_webhook_endpoints(id),
ciphertext text NOT NULL CHECK (octet_length(ciphertext) BETWEEN 1 AND 4096),
expires_at timestamptz CHECK (expires_at IS NULL OR isfinite(expires_at)),
created_at timestamptz NOT NULL CHECK (isfinite(created_at))
)`,
		`CREATE INDEX foundry_webhook_secrets_endpoint ON foundry_webhook_secrets (endpoint_id,created_at)`,
		`CREATE TABLE foundry_webhook_deliveries (
id uuid PRIMARY KEY,
endpoint_id uuid NOT NULL REFERENCES foundry_webhook_endpoints(id),
event text NOT NULL CHECK (octet_length(event) BETWEEN 1 AND 128),
payload text NOT NULL CHECK (octet_length(payload) BETWEEN 2 AND 1048576),
state text NOT NULL CHECK (state IN ('pending','succeeded','failed')),
attempts bigint NOT NULL CHECK (attempts BETWEEN 0 AND 4294967295),
last_status integer NOT NULL CHECK (last_status=0 OR last_status BETWEEN 100 AND 599),
last_failure text NOT NULL CHECK (octet_length(last_failure)<=64),
created_at timestamptz NOT NULL CHECK (isfinite(created_at)),
updated_at timestamptz NOT NULL CHECK (isfinite(updated_at)),
delivered_at timestamptz CHECK (delivered_at IS NULL OR isfinite(delivered_at))
)`,
		`CREATE INDEX foundry_webhook_deliveries_endpoint ON foundry_webhook_deliveries (endpoint_id,state,id)`,
		`CREATE INDEX foundry_webhook_deliveries_state ON foundry_webhook_deliveries (state,id)`}}}
}
