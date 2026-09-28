package postgres

import "github.com/weiloon1234/Foundry-Go/database/migrate"

const (
	MigrationOrigin migrate.Origin  = "foundry.mfa"
	CreateFactors   migrate.ID      = "000001_create_factors"
	Introduced      migrate.Version = "v0.1.0"
)

// Migrations must be applied explicitly alongside the application's migrations.
// No constructor mutates schemas. Each row stores encrypted factor material and
// hashes only; no plaintext TOTP/recovery code is stored.
func Migrations() []migrate.Definition {
	return []migrate.Definition{{Key: migrate.Key{Origin: MigrationOrigin, ID: CreateFactors}, Version: Introduced, SQL: []string{
		`CREATE TABLE foundry_mfa_factors (
key text PRIMARY KEY CHECK (key ~ '^[0-9a-f]{64}$'),
scope text NOT NULL CHECK (scope ~ '^[0-9a-f]{64}$'),
identity jsonb NOT NULL CHECK (octet_length(identity::text) <= 8192),
generation uuid NOT NULL CHECK (generation <> '00000000-0000-0000-0000-000000000000'),
ciphertext text NOT NULL CHECK (octet_length(ciphertext) BETWEEN 1 AND 512),
created_at timestamptz NOT NULL CHECK (isfinite(created_at) AND created_at >= timestamp with time zone '1970-01-01 00:00:00+00'),
pending_until timestamptz,
confirmed_at timestamptz,
last_step bigint,
recovery_hashes jsonb NOT NULL CHECK (jsonb_typeof(recovery_hashes) = 'array' AND jsonb_array_length(recovery_hashes) <= 16 AND octet_length(recovery_hashes::text) <= 1200),
CHECK (
 (pending_until IS NOT NULL AND isfinite(pending_until) AND pending_until - created_at BETWEEN interval '1 millisecond' AND interval '1 hour' AND confirmed_at IS NULL AND last_step IS NULL AND jsonb_array_length(recovery_hashes) = 0)
 OR
 (pending_until IS NULL AND confirmed_at IS NOT NULL AND isfinite(confirmed_at) AND confirmed_at >= created_at AND last_step IS NOT NULL AND last_step BETWEEN 0 AND 8446743359)
)
)`,
		`CREATE INDEX foundry_mfa_pending_expiry ON foundry_mfa_factors (scope, pending_until, key) WHERE pending_until IS NOT NULL`,
	}}}
}
