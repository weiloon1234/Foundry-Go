package translations

import "github.com/weiloon1234/Foundry-Go/database/migrate"

const (
	MigrationOrigin    migrate.Origin = "foundry.translations"
	CreateTranslations migrate.ID     = "000001_create_translations"
	// IndexValues adds a hash index for Matching's exact-value lookups. Hash
	// entries store only a hash code, so 64 KiB values stay indexable, unlike
	// a btree over the text itself.
	IndexValues migrate.ID      = "000002_index_translation_values"
	Introduced  migrate.Version = "v0.1.0"
)

// Migrations are explicit and ordered; applied definitions are immutable, so
// later schema changes are appended as new migrations.
func Migrations() []migrate.Definition {
	created := migrate.Key{Origin: MigrationOrigin, ID: CreateTranslations}
	return []migrate.Definition{{Key: created, Version: Introduced, SQL: []string{`CREATE TABLE foundry_model_translations (
key text PRIMARY KEY CHECK (key ~ '^[0-9a-f]{64}$'),
owner text NOT NULL,
scope text NOT NULL CHECK (scope ~ '^[0-9a-f]{64}$'),
subject_key text NOT NULL CHECK (subject_key ~ '^[0-9a-f]{64}$'),
identity jsonb NOT NULL CHECK (octet_length(identity::text)<=8192),
field text NOT NULL,
locale text NOT NULL,
value text NOT NULL CHECK (octet_length(value)<=65536),
created_at timestamptz NOT NULL CHECK (isfinite(created_at)),
updated_at timestamptz NOT NULL CHECK (isfinite(updated_at)),
UNIQUE (scope,subject_key,field,locale)
)`, `CREATE INDEX foundry_model_translations_owner ON foundry_model_translations (owner,key)`, `CREATE INDEX foundry_model_translations_field ON foundry_model_translations (scope,field,locale,key)`}},
		{Key: migrate.Key{Origin: MigrationOrigin, ID: IndexValues}, Version: Introduced, Requires: []migrate.Key{created}, SQL: []string{
			`CREATE INDEX foundry_model_translations_value ON foundry_model_translations USING hash (value)`,
		}},
	}
}
