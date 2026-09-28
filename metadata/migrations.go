package metadata

import "github.com/weiloon1234/Foundry-Go/database/migrate"

const (
	MigrationOrigin migrate.Origin  = "foundry.metadata"
	CreateMetadata  migrate.ID      = "000001_create_metadata"
	Introduced      migrate.Version = "v0.1.0"
)

// Migrations are explicit and use the common Store's schema. No automatic boot
// migration or owner-table inference occurs.
func Migrations() []migrate.Definition {
	return []migrate.Definition{{Key: migrate.Key{Origin: MigrationOrigin, ID: CreateMetadata}, Version: Introduced, SQL: []string{
		`CREATE TABLE foundry_model_metadata (
key text PRIMARY KEY CHECK (key ~ '^[0-9a-f]{64}$'),
owner text NOT NULL,
scope text NOT NULL CHECK (scope ~ '^[0-9a-f]{64}$'),
subject_key text NOT NULL CHECK (subject_key ~ '^[0-9a-f]{64}$'),
identity jsonb NOT NULL CHECK (octet_length(identity::text)<=8192),
name text NOT NULL,
version bigint NOT NULL CHECK (version BETWEEN 1 AND 4294967295),
value jsonb NOT NULL CHECK (octet_length(value::text)<=1048576),
created_at timestamptz NOT NULL CHECK (isfinite(created_at)),
updated_at timestamptz NOT NULL CHECK (isfinite(updated_at)),
UNIQUE (scope,subject_key,name)
)`,
		`CREATE INDEX foundry_model_metadata_owner ON foundry_model_metadata (owner,key)`,
		`CREATE INDEX foundry_model_metadata_name ON foundry_model_metadata (scope,name,key)`,
	}}}
}
