package postgres

import "github.com/weiloon1234/Foundry-Go/database/migrate"

const (
	MigrationOrigin  migrate.Origin  = "foundry.challenges"
	CreateChallenges migrate.ID      = "000001_create_challenges"
	Introduced       migrate.Version = "v0.1.0"
)

// Migrations must be registered and applied explicitly. Subject rows are stable
// synchronization records and are not garbage-collected by ordinary pruning.
func Migrations() []migrate.Definition {
	return []migrate.Definition{{Key: migrate.Key{Origin: MigrationOrigin, ID: CreateChallenges}, Version: Introduced, SQL: []string{
		`CREATE TABLE foundry_challenge_subjects (
key text PRIMARY KEY CHECK (key ~ '^[0-9a-f]{64}$'),
scope text NOT NULL CHECK (scope ~ '^[0-9a-f]{64}$'),
identity jsonb NOT NULL CHECK (octet_length(identity::text) <= 8192),
UNIQUE (key, scope)
)`,
		`CREATE TABLE foundry_challenges (
id uuid PRIMARY KEY,
scope text NOT NULL CHECK (scope ~ '^[0-9a-f]{64}$'),
subject_key text NOT NULL UNIQUE,
secret_hash text NOT NULL CHECK (secret_hash ~ '^[0-9a-f]{64}$'),
binding_hash text NOT NULL CHECK (binding_hash ~ '^[0-9a-f]{64}$'),
created_at timestamptz NOT NULL,
expires_at timestamptz NOT NULL,
FOREIGN KEY (subject_key, scope) REFERENCES foundry_challenge_subjects (key, scope),
UNIQUE (scope, secret_hash),
CHECK (isfinite(created_at) AND isfinite(expires_at) AND expires_at > created_at AND expires_at - created_at BETWEEN interval '1 millisecond' AND interval '168 hours')
)`,
		`CREATE INDEX foundry_challenge_expiry ON foundry_challenges (scope, expires_at, subject_key, id)`,
	}}}
}
