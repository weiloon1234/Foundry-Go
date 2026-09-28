package outbox

import "github.com/weiloon1234/Foundry-Go/database/migrate"

const (
	MigrationOrigin migrate.Origin  = "foundry.outbox"
	CreateMessages  migrate.ID      = "000001_create_messages"
	AddPublication  migrate.ID      = "000002_add_publication"
	Introduced      migrate.Version = "v0.1.0"
)

// Migrations returns owned historical definitions for the shared outbox table.
// Register them once with application/plugin definitions and run the ordinary
// migration command explicitly. Calling this function performs no I/O. The
// table follows the connection's selected schema, as ordinary model tables do;
// producers and the migration runner must use the same intended database/schema.
// Later delivery capabilities add new migrations without rewriting this history.
func Migrations() []migrate.Definition {
	return []migrate.Definition{{
		Key:     migrate.Key{Origin: MigrationOrigin, ID: CreateMessages},
		Version: Introduced,
		SQL: []string{
			`CREATE TABLE foundry_outbox (
id uuid PRIMARY KEY,
kind text NOT NULL CHECK (octet_length(kind) BETWEEN 1 AND 128),
destination text NOT NULL CHECK (octet_length(destination) BETWEEN 1 AND 128),
name text NOT NULL CHECK (octet_length(name) BETWEEN 1 AND 128),
version bigint NOT NULL CHECK (version BETWEEN 1 AND 4294967295),
payload jsonb NOT NULL,
origin jsonb NOT NULL,
created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
)`,
			`CREATE INDEX foundry_outbox_order ON foundry_outbox (kind, destination, created_at, id)`,
		},
	}, {
		Key: migrate.Key{Origin: MigrationOrigin, ID: AddPublication}, Version: Introduced,
		SQL: []string{
			`ALTER TABLE foundry_outbox
ADD COLUMN publish_state text NOT NULL DEFAULT 'pending' CHECK (publish_state IN ('pending','published','failed')),
ADD COLUMN publish_attempts bigint NOT NULL DEFAULT 0 CHECK (publish_attempts BETWEEN 0 AND 4294967295),
ADD COLUMN publish_after timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
ADD COLUMN published_at timestamptz NULL,
ADD COLUMN publish_reason text NOT NULL DEFAULT '' CHECK (publish_reason IN ('','transient','permanent','attempt_limit'))`,
			`CREATE INDEX foundry_outbox_publication ON foundry_outbox (kind, destination, publish_after, created_at, id) WHERE publish_state = 'pending'`,
		},
	}}
}
