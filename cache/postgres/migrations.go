package postgres

import "github.com/weiloon1234/Foundry-Go/database/migrate"

const MigrationOrigin migrate.Origin = "foundry.cache"
const CreateCache migrate.ID = "000001_create_cache"

// Migrations are applied explicitly by the ordinary runner in Config.Schema.
// Table rows are isolated by complete application/environment/tenant addresses.
func Migrations() []migrate.Definition {
	return []migrate.Definition{{Key: migrate.Key{Origin: MigrationOrigin, ID: CreateCache}, Version: "v0.1.0", SQL: []string{
		`CREATE TABLE foundry_cache_entries (
 key text PRIMARY KEY CHECK(key ~ '^[0-9a-f]{64}$'),
 address text NOT NULL CHECK(octet_length(address) BETWEEN 1 AND 16384),
 payload bytea NOT NULL CHECK(octet_length(payload) <= 67108864),
 expires_at timestamptz CHECK(expires_at IS NULL OR isfinite(expires_at))
 )`,
		`CREATE INDEX foundry_cache_expiry ON foundry_cache_entries (expires_at,key) WHERE expires_at IS NOT NULL`,
	}}}
}
