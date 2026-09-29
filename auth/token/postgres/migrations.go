// Package postgres implements authoritative token persistence through explicit
// Foundry migrations and generated model queries. Construction performs no I/O.
package postgres

import "github.com/weiloon1234/Foundry-Go/database/migrate"

const (
	MigrationOrigin migrate.Origin = "foundry.tokens"
	CreateTokens    migrate.ID     = "000001_create_tokens"
	AddTokenState   migrate.ID     = "000002_token_device_refresh_history"
	// ValidateTokenState validates 000002's NOT VALID checks in a separate transaction.
	ValidateTokenState migrate.ID      = "000003_validate_token_state"
	Introduced         migrate.Version = "v0.1.0"
)

// Migrations returns immutable definitions for the schema selected by Config.
// Apply explicitly through the ordinary runner; no startup schema synchronization.
func Migrations() []migrate.Definition {
	return []migrate.Definition{{Key: migrate.Key{Origin: MigrationOrigin, ID: CreateTokens}, Version: Introduced, SQL: []string{
		`CREATE TABLE foundry_token_subjects (
key text PRIMARY KEY CHECK (key ~ '^[0-9a-f]{64}$'),
scope text NOT NULL CHECK (scope ~ '^[0-9a-f]{64}$'),
identity jsonb NOT NULL CHECK (octet_length(identity::text) <= 8192),
UNIQUE (key, scope)
)`,
		`CREATE TABLE foundry_token_families (
id uuid PRIMARY KEY,
scope text NOT NULL CHECK (scope ~ '^[0-9a-f]{64}$'),
subject_key text NOT NULL,
name text NOT NULL CHECK (octet_length(name) <= 128),
scopes jsonb NOT NULL CHECK (jsonb_typeof(scopes) = 'array' AND jsonb_array_length(scopes) <= 128 AND octet_length(scopes::text) <= 24576),
mode smallint NOT NULL CHECK (mode IN (1, 2, 3)),
assurance smallint NOT NULL CHECK (assurance IN (1, 2)),
access_nanos bigint NOT NULL CHECK (access_nanos BETWEEN 1000000 AND 31536000000000000 AND access_nanos % 1000 = 0),
refresh_idle_nanos bigint NOT NULL CHECK (refresh_idle_nanos BETWEEN 0 AND 31536000000000000 AND refresh_idle_nanos % 1000 = 0),
rotation_limit bigint NOT NULL CHECK (rotation_limit BETWEEN 0 AND 4096),
generation bigint NOT NULL CHECK (generation >= 0 AND generation <= rotation_limit),
created_at timestamptz NOT NULL,
expires_at timestamptz NOT NULL,
FOREIGN KEY (subject_key, scope) REFERENCES foundry_token_subjects (key, scope),
UNIQUE (id, scope),
CHECK (isfinite(created_at) AND isfinite(expires_at) AND expires_at > created_at AND expires_at - created_at <= interval '8760 hours'),
CHECK ((mode = 2 AND assurance = 2 AND refresh_idle_nanos >= access_nanos AND rotation_limit >= 1)
OR (mode = 1 AND assurance = 2 AND refresh_idle_nanos = 0 AND rotation_limit = 0)
OR (mode = 3 AND assurance = 1 AND refresh_idle_nanos = 0 AND rotation_limit = 0 AND scopes = '[]'::jsonb AND expires_at - created_at <= interval '15 minutes'))
)`,
		`CREATE TABLE foundry_token_generations (
id uuid PRIMARY KEY,
scope text NOT NULL CHECK (scope ~ '^[0-9a-f]{64}$'),
family_id uuid NOT NULL,
generation bigint NOT NULL CHECK (generation BETWEEN 0 AND 4096),
access_hash text NOT NULL CHECK (access_hash ~ '^[0-9a-f]{64}$'),
refresh_hash text CHECK (refresh_hash ~ '^[0-9a-f]{64}$' AND refresh_hash <> access_hash),
issued_at timestamptz NOT NULL,
last_seen_at timestamptz NOT NULL,
access_expires_at timestamptz NOT NULL,
refresh_expires_at timestamptz,
FOREIGN KEY (family_id, scope) REFERENCES foundry_token_families (id, scope) ON DELETE CASCADE,
UNIQUE (scope, access_hash),
UNIQUE (scope, refresh_hash),
UNIQUE (family_id, generation),
CHECK (isfinite(issued_at) AND isfinite(last_seen_at) AND isfinite(access_expires_at)),
CHECK (last_seen_at >= issued_at AND access_expires_at > last_seen_at),
CHECK ((refresh_hash IS NULL AND refresh_expires_at IS NULL AND generation = 0)
OR (refresh_hash IS NOT NULL AND refresh_expires_at IS NOT NULL AND isfinite(refresh_expires_at) AND refresh_expires_at >= access_expires_at))
)`,
		`CREATE INDEX foundry_token_family_subject_order ON foundry_token_families (scope, subject_key, created_at, id)`,
		`CREATE INDEX foundry_token_family_expiry ON foundry_token_families (scope, expires_at, id)`,
		`CREATE INDEX foundry_token_generation_expiry ON foundry_token_generations (scope, refresh_expires_at, access_expires_at, family_id)`,
	}}, {Key: migrate.Key{Origin: MigrationOrigin, ID: AddTokenState}, Version: Introduced, Requires: []migrate.Key{{Origin: MigrationOrigin, ID: CreateTokens}}, SQL: []string{
		// Nullable columns without defaults are metadata-only additions. Their
		// checks are added NOT VALID (no scan under the ACCESS EXCLUSIVE lock) and
		// validated by 000003 in its own transaction, which blocks no reads or writes.
		`ALTER TABLE foundry_token_families ADD COLUMN client_ip text`,
		`ALTER TABLE foundry_token_families ADD COLUMN user_agent text`,
		`ALTER TABLE foundry_token_families ADD CONSTRAINT foundry_token_families_client_ip CHECK (client_ip IS NULL OR octet_length(client_ip) BETWEEN 1 AND 64) NOT VALID`,
		`ALTER TABLE foundry_token_families ADD CONSTRAINT foundry_token_families_user_agent CHECK (user_agent IS NULL OR octet_length(user_agent) BETWEEN 1 AND 512) NOT VALID`,
		// Refresh digests consumed before a family's previous generation. Refresh
		// moves them here and deletes their generation rows; reuse still revokes.
		`CREATE TABLE foundry_token_consumed_refreshes (
refresh_hash text PRIMARY KEY CHECK (refresh_hash ~ '^[0-9a-f]{64}$'),
family_id uuid NOT NULL REFERENCES foundry_token_families (id) ON DELETE CASCADE
)`,
		`CREATE INDEX foundry_token_consumed_refresh_family ON foundry_token_consumed_refreshes (family_id)`,
	}}, {Key: migrate.Key{Origin: MigrationOrigin, ID: ValidateTokenState}, Version: Introduced, Requires: []migrate.Key{{Origin: MigrationOrigin, ID: AddTokenState}}, SQL: []string{
		`ALTER TABLE foundry_token_families VALIDATE CONSTRAINT foundry_token_families_client_ip`,
		`ALTER TABLE foundry_token_families VALIDATE CONSTRAINT foundry_token_families_user_agent`,
	}}}
}
