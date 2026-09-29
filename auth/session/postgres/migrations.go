package postgres

import "github.com/weiloon1234/Foundry-Go/database/migrate"

const (
	MigrationOrigin migrate.Origin  = "foundry.sessions"
	CreateSessions  migrate.ID      = "000001_create_sessions"
	AddSessionState migrate.ID      = "000002_session_device_confirmation"
	AddImpersonator migrate.ID      = "000003_session_impersonation"
	ValidateState   migrate.ID      = "000004_validate_session_state"
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
	}}, {Key: migrate.Key{Origin: MigrationOrigin, ID: AddSessionState}, Version: Introduced, Requires: []migrate.Key{{Origin: MigrationOrigin, ID: CreateSessions}}, SQL: []string{
		// Nullable columns without defaults are metadata-only additions. Their
		// checks are added NOT VALID (no scan under the ACCESS EXCLUSIVE lock) and
		// validated by 000004 in its own transaction, which blocks no reads or writes.
		`ALTER TABLE foundry_sessions ADD COLUMN client_ip text`,
		`ALTER TABLE foundry_sessions ADD COLUMN user_agent text`,
		`ALTER TABLE foundry_sessions ADD COLUMN confirmed_at timestamptz`,
		`ALTER TABLE foundry_sessions ADD CONSTRAINT foundry_sessions_client_ip CHECK (client_ip IS NULL OR octet_length(client_ip) BETWEEN 1 AND 64) NOT VALID`,
		`ALTER TABLE foundry_sessions ADD CONSTRAINT foundry_sessions_user_agent CHECK (user_agent IS NULL OR octet_length(user_agent) BETWEEN 1 AND 512) NOT VALID`,
		`ALTER TABLE foundry_sessions ADD CONSTRAINT foundry_sessions_confirmed_at CHECK (confirmed_at IS NULL OR (isfinite(confirmed_at) AND confirmed_at >= created_at AND assurance = 2)) NOT VALID`,
	}}, {Key: migrate.Key{Origin: MigrationOrigin, ID: AddImpersonator}, Version: Introduced, Requires: []migrate.Key{{Origin: MigrationOrigin, ID: AddSessionState}}, SQL: []string{
		`ALTER TABLE foundry_sessions ADD COLUMN impersonator_identity text`,
		`ALTER TABLE foundry_sessions ADD COLUMN impersonator_guard text`,
		`ALTER TABLE foundry_sessions ADD COLUMN impersonator_session uuid`,
		`ALTER TABLE foundry_sessions ADD CONSTRAINT foundry_sessions_impersonator_identity CHECK (impersonator_identity IS NULL OR octet_length(impersonator_identity) BETWEEN 2 AND 8192) NOT VALID`,
		`ALTER TABLE foundry_sessions ADD CONSTRAINT foundry_sessions_impersonator_guard CHECK (impersonator_guard IS NULL OR octet_length(impersonator_guard) BETWEEN 1 AND 128) NOT VALID`,
		`ALTER TABLE foundry_sessions ADD CONSTRAINT foundry_sessions_impersonation CHECK ((impersonator_identity IS NULL AND impersonator_guard IS NULL AND impersonator_session IS NULL) OR (impersonator_identity IS NOT NULL AND impersonator_guard IS NOT NULL AND impersonator_session IS NOT NULL AND assurance = 2 AND NOT remember AND confirmed_at IS NULL)) NOT VALID`,
	}}, {Key: migrate.Key{Origin: MigrationOrigin, ID: ValidateState}, Version: Introduced, Requires: []migrate.Key{{Origin: MigrationOrigin, ID: AddImpersonator}}, SQL: []string{
		// VALIDATE takes SHARE UPDATE EXCLUSIVE: sessions keep being read and
		// written while existing rows are checked.
		`ALTER TABLE foundry_sessions VALIDATE CONSTRAINT foundry_sessions_client_ip`,
		`ALTER TABLE foundry_sessions VALIDATE CONSTRAINT foundry_sessions_user_agent`,
		`ALTER TABLE foundry_sessions VALIDATE CONSTRAINT foundry_sessions_confirmed_at`,
		`ALTER TABLE foundry_sessions VALIDATE CONSTRAINT foundry_sessions_impersonator_identity`,
		`ALTER TABLE foundry_sessions VALIDATE CONSTRAINT foundry_sessions_impersonator_guard`,
		`ALTER TABLE foundry_sessions VALIDATE CONSTRAINT foundry_sessions_impersonation`,
	}}}
}
