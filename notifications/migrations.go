package notifications

import "github.com/weiloon1234/Foundry-Go/database/migrate"

const (
	MigrationOrigin     migrate.Origin  = "foundry.notifications"
	CreateNotifications migrate.ID      = "000001_create_notifications"
	Introduced          migrate.Version = "v0.1.0"
)

// Migrations are explicit ordinary migrations. Use the same schema as Config;
// constructing a manager never connects, migrates, resets or drops existing data.
func Migrations() []migrate.Definition {
	return []migrate.Definition{{Key: migrate.Key{Origin: MigrationOrigin, ID: CreateNotifications}, Version: Introduced, SQL: []string{
		`CREATE TABLE foundry_notifications (
id uuid PRIMARY KEY,
recipient text NOT NULL,
scope text NOT NULL CHECK (scope ~ '^[0-9a-f]{64}$'),
subject_key text NOT NULL CHECK (subject_key ~ '^[0-9a-f]{64}$'),
identity jsonb NOT NULL CHECK (octet_length(identity::text) <= 8192),
origin jsonb NOT NULL CHECK (octet_length(origin::text) <= 16384),
name text NOT NULL,
version bigint NOT NULL CHECK (version BETWEEN 1 AND 4294967295),
payload jsonb NOT NULL CHECK (octet_length(payload::text) <= 1048576),
fingerprint text NOT NULL CHECK (fingerprint ~ '^[0-9a-f]{64}$'),
created_at timestamptz NOT NULL CHECK (isfinite(created_at)),
UNIQUE (id, scope, subject_key)
)`,
		`CREATE TABLE foundry_notification_deliveries (
key text PRIMARY KEY CHECK (key ~ '^[0-9a-f]{64}$'),
notification_id uuid NOT NULL REFERENCES foundry_notifications (id),
channel text NOT NULL,
kind text NOT NULL CHECK (kind IN ('database','email','realtime','custom')),
state text NOT NULL CHECK (state IN ('pending','prepared','running','delivered','skipped','ineligible','rejected','uncertain')),
payload jsonb NOT NULL CHECK (octet_length(payload::text) <= 1048576),
claim uuid NOT NULL,
attempts bigint NOT NULL CHECK (attempts BETWEEN 0 AND 4294967295),
updated_at timestamptz NOT NULL CHECK (isfinite(updated_at)),
UNIQUE (notification_id, channel)
)`,
		`CREATE TABLE foundry_notification_inbox (
id uuid PRIMARY KEY,
scope text NOT NULL,
subject_key text NOT NULL,
name text NOT NULL,
version bigint NOT NULL CHECK (version BETWEEN 1 AND 4294967295),
data jsonb NOT NULL CHECK (octet_length(data::text) <= 1048576),
created_at timestamptz NOT NULL CHECK (isfinite(created_at)),
read_at timestamptz CHECK (isfinite(read_at)),
FOREIGN KEY (id, scope, subject_key) REFERENCES foundry_notifications (id, scope, subject_key)
)`,
		`CREATE INDEX foundry_notification_inbox_subject ON foundry_notification_inbox (scope, subject_key, created_at, id)`,
		`CREATE INDEX foundry_notification_inbox_unread ON foundry_notification_inbox (scope, subject_key, created_at, id) WHERE read_at IS NULL`,
		`CREATE INDEX foundry_notification_delivery_state ON foundry_notification_deliveries (state, updated_at, key)`,
	}}}
}
