package audit

import "github.com/weiloon1234/Foundry-Go/database/migrate"

const (
	MigrationOrigin migrate.Origin  = "foundry.audit"
	CreateEntries   migrate.ID      = "000001_create_entries"
	Introduced      migrate.Version = "v0.1.0"
)

// Migrations returns owned historical definitions. Register and run them with
// the ordinary explicit migration command against the recorder's database/schema.
// Missing storage fails enabled audit writes instead of silently discarding them.
func Migrations() []migrate.Definition {
	return []migrate.Definition{{
		Key:     migrate.Key{Origin: MigrationOrigin, ID: CreateEntries},
		Version: Introduced,
		SQL: []string{
			`CREATE TABLE foundry_audit (
id uuid PRIMARY KEY,
area text NOT NULL CHECK (octet_length(area) BETWEEN 1 AND 128),
operation smallint NOT NULL,
action text NOT NULL,
version bigint NOT NULL CHECK (version BETWEEN 0 AND 4294967295),
subject jsonb,
payload jsonb NOT NULL,
redacted boolean NOT NULL,
origin jsonb NOT NULL,
created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
CHECK ((operation BETWEEN 1 AND 6 AND action = '' AND version = 0 AND subject IS NOT NULL AND NOT redacted)
    OR (operation = 0 AND octet_length(action) BETWEEN 1 AND 128 AND version > 0))
)`,
			`CREATE INDEX foundry_audit_order ON foundry_audit (area, created_at, id)`,
			`CREATE INDEX foundry_audit_subject ON foundry_audit USING HASH (subject)`,
		},
	}}
}
