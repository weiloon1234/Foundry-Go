package idempotency

import "github.com/weiloon1234/Foundry-Go/database/migrate"

const MigrationOrigin migrate.Origin = "foundry.idempotency"
const CreateOutcomes migrate.ID = "000001_create_outcomes"

// Migrations describes the shared store. Install explicitly with ordinary
// migrations; constructors never mutate schemas or existing data.
func Migrations() []migrate.Definition {
	return []migrate.Definition{{Key: migrate.Key{Origin: MigrationOrigin, ID: CreateOutcomes}, Version: "v0.1.0", SQL: []string{
		`CREATE TABLE foundry_idempotency (
id uuid PRIMARY KEY,
namespace text NOT NULL CHECK (octet_length(namespace) BETWEEN 1 AND 128),
operation text NOT NULL CHECK (octet_length(operation) BETWEEN 1 AND 128),
version bigint NOT NULL CHECK (version BETWEEN 1 AND 4294967295),
scope_digest text NOT NULL CHECK (scope_digest ~ '^[0-9a-f]{64}$'),
key_digest text NOT NULL CHECK (key_digest ~ '^[0-9a-f]{64}$'),
fingerprint text NOT NULL CHECK (fingerprint ~ '^[0-9a-f]{64}$'),
result_schema text NOT NULL DEFAULT '' CHECK (octet_length(result_schema) <= 256),
result_hash text NOT NULL DEFAULT '' CHECK (result_hash = '' OR result_hash ~ '^[0-9a-f]{64}$'),
representation bytea NOT NULL DEFAULT ''::bytea CHECK (octet_length(representation) <= 1048576),
completed_at timestamptz NULL,
expires_at timestamptz NULL,
CONSTRAINT foundry_idempotency_address UNIQUE (namespace, operation, version, scope_digest, key_digest),
CHECK ((completed_at IS NULL AND expires_at IS NULL AND result_schema = '' AND result_hash = '' AND octet_length(representation)=0) OR (completed_at IS NOT NULL AND expires_at IS NOT NULL AND expires_at > completed_at AND result_schema <> '' AND result_hash <> '' AND octet_length(representation)>0))
)`,
		`CREATE INDEX foundry_idempotency_caller ON foundry_idempotency (namespace, scope_digest)`,
		`CREATE INDEX foundry_idempotency_expiry ON foundry_idempotency (namespace, expires_at, id) WHERE completed_at IS NOT NULL`,
	}}}
}
