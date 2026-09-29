package settings

import "github.com/weiloon1234/Foundry-Go/database/migrate"

const (
	MigrationOrigin          migrate.Origin  = "foundry.settings"
	CreateSettings           migrate.ID      = "000001_create_settings"
	AddPresentationOwnership migrate.ID      = "000002_add_presentation_ownership"
	Introduced               migrate.Version = "v0.1.0"
)

// Migrations returns the settings table history. Rows existing before
// presentation ownership keep their stored presentation (declared=false);
// ResetPresentation hands a row back to its declaration explicitly.
func Migrations() []migrate.Definition {
	return []migrate.Definition{{Key: migrate.Key{Origin: MigrationOrigin, ID: CreateSettings}, Version: Introduced, SQL: []string{`CREATE TABLE foundry_settings (
name text PRIMARY KEY CHECK (octet_length(name) BETWEEN 1 AND 128),
version bigint NOT NULL CHECK (version BETWEEN 1 AND 4294967295),
value jsonb NOT NULL CHECK (octet_length(value::text)<=1048576),
kind text NOT NULL,
parameters jsonb NOT NULL CHECK (jsonb_typeof(parameters)='object' AND octet_length(parameters::text)<=65536),
group_name text NOT NULL,
label text NOT NULL,
description text NOT NULL,
sort_order integer NOT NULL,
is_public boolean NOT NULL,
created_at timestamptz NOT NULL CHECK (isfinite(created_at)),
updated_at timestamptz NOT NULL CHECK (isfinite(updated_at))
)`, `CREATE INDEX foundry_settings_presentation ON foundry_settings (group_name,sort_order,name)`}}, {
		Key: migrate.Key{Origin: MigrationOrigin, ID: AddPresentationOwnership}, Version: Introduced,
		SQL: []string{`ALTER TABLE foundry_settings ADD COLUMN presentation_declared boolean NOT NULL DEFAULT false`},
	}}
}
