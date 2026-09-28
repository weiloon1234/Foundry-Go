package postgres

import "github.com/weiloon1234/Foundry-Go/database/migrate"

const (
	MigrationOrigin migrate.Origin  = "foundry.sessions"
	CreateSessions  migrate.ID      = "000001_create_sessions"
	Introduced      migrate.Version = "v0.1.0"
)

// Migrations returns owned historical definitions. Run them explicitly in the
// same schema as Config.Schema using the ordinary migration runner. No existing
// table/data is dropped or reset, and construction never auto-synchronizes schema.
func Migrations() []migrate.Definition {
	return []migrate.Definition{{Key: migrate.Key{Origin: MigrationOrigin, ID: CreateSessions}, Version: Introduced, SQL: []string{
		`CREATE TABLE foundry_session_subjects (
key text PRIMARY KEY CHECK (key ~ '^[0-9a-f]{64}$'),
scope text NOT NULL CHECK (scope ~ '^[0-9a-f]{64}$'),
identity jsonb NOT NULL CHECK (octet_length(identity::text) <= 8192),
UNIQUE (key, scope)
)`,
		`CREATE TABLE foundry_sessions (
id uuid PRIMARY KEY,
scope text NOT NULL CHECK (scope ~ '^[0-9a-f]{64}$'),
subject_key text NOT NULL,
secret_hash text NOT NULL CHECK (secret_hash ~ '^[0-9a-f]{64}$'),
assurance smallint NOT NULL CHECK (assurance IN (1, 2)),
remember boolean NOT NULL,
sliding boolean NOT NULL,
idle_nanos bigint NOT NULL CHECK (idle_nanos BETWEEN 1000000 AND 31536000000000000 AND idle_nanos % 1000 = 0),
created_at timestamptz NOT NULL,
last_seen_at timestamptz NOT NULL,
idle_expires_at timestamptz NOT NULL,
expires_at timestamptz NOT NULL,
FOREIGN KEY (subject_key, scope) REFERENCES foundry_session_subjects (key, scope),
UNIQUE (scope, secret_hash),
CHECK (isfinite(created_at) AND isfinite(last_seen_at) AND isfinite(idle_expires_at) AND isfinite(expires_at)),
CHECK (last_seen_at >= created_at AND idle_expires_at > last_seen_at AND expires_at >= idle_expires_at),
CHECK (expires_at - created_at <= interval '8760 hours'),
CHECK (assurance <> 1 OR (NOT remember AND NOT sliding))
)`,
		`CREATE INDEX foundry_sessions_subject_order ON foundry_sessions (scope, subject_key, created_at, id)`,
		`CREATE INDEX foundry_sessions_idle_expiry ON foundry_sessions (scope, idle_expires_at, id)`,
		`CREATE INDEX foundry_sessions_absolute_expiry ON foundry_sessions (scope, expires_at, id)`,
	}}}
}
