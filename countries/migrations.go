package countries

import "github.com/weiloon1234/Foundry-Go/database/migrate"

const (
	MigrationOrigin migrate.Origin  = "foundry.countries"
	CreateCountries migrate.ID      = "000001_create_countries"
	Introduced      migrate.Version = "v0.1.0"
)

func Migrations() []migrate.Definition {
	return []migrate.Definition{{Key: migrate.Key{Origin: MigrationOrigin, ID: CreateCountries}, Version: Introduced, SQL: []string{`CREATE TABLE foundry_countries (
iso2 text PRIMARY KEY CHECK (iso2 ~ '^[A-Z]{2}$'),
iso3 text NOT NULL UNIQUE CHECK (iso3 ~ '^[A-Z]{3}$'),
iso_numeric text CHECK (iso_numeric ~ '^[0-9]{3}$'),
name text NOT NULL CHECK (octet_length(name) BETWEEN 1 AND 1024),
official_name text,
capital text,
region text,
subregion text,
currencies jsonb NOT NULL CHECK (jsonb_typeof(currencies)='array' AND octet_length(currencies::text)<=65536),
primary_currency_code text,
calling_code text,
calling_root text,
calling_suffixes jsonb NOT NULL CHECK (jsonb_typeof(calling_suffixes)='array' AND octet_length(calling_suffixes::text)<=65536),
tlds jsonb NOT NULL CHECK (jsonb_typeof(tlds)='array' AND octet_length(tlds::text)<=65536),
timezones jsonb NOT NULL CHECK (jsonb_typeof(timezones)='array' AND octet_length(timezones::text)<=65536),
latitude double precision CHECK (latitude BETWEEN -90 AND 90),
longitude double precision CHECK (longitude BETWEEN -180 AND 180),
independent boolean,
un_member boolean,
flag_emoji text,
status text NOT NULL CHECK (status IN ('enabled','disabled')),
conversion_rate numeric CHECK (conversion_rate>=0 AND conversion_rate<'Infinity'::numeric),
is_default boolean NOT NULL,
reference_version text NOT NULL,
created_at timestamptz NOT NULL CHECK (isfinite(created_at)),
updated_at timestamptz NOT NULL CHECK (isfinite(updated_at))
)`, `CREATE INDEX foundry_countries_status ON foundry_countries (status,name,iso2)`}}}
}
