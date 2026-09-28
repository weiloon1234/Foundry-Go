package infrastructure

import (
	"github.com/weiloon1234/Foundry-Go/cache"
	cachefile "github.com/weiloon1234/Foundry-Go/cache/file"
	"github.com/weiloon1234/Foundry-Go/cache/memory"
	cachepg "github.com/weiloon1234/Foundry-Go/cache/postgres"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/redis"
)

type CacheDriver string

const (
	MemoryCache   CacheDriver = "memory"
	RedisCache    CacheDriver = "redis"
	FileCache     CacheDriver = "file"
	PostgresCache CacheDriver = "postgres"
)

type FileCacheSettings struct {
	Root       string
	MaxEntries int
	MaxBytes   int64
	Sync       bool
}
type PostgresCacheSettings struct {
	Schema     string
	MaxEntries int
	MaxBytes   int64
}

// CacheSettings chooses an adapter while domain declarations keep their types.
// An omitted Redis/Database reference selects that family's configured default.
//
//foundry:config
type CacheSettings struct {
	Driver       CacheDriver
	Config       cache.Config
	Memory       memory.Config
	File         FileCacheSettings
	Postgres     PostgresCacheSettings
	Redis        redis.ConnectionName
	Database     database.ConnectionName
	Require      cache.Requirements
	Leases       lease.Config
	Coordination cache.CoordinationConfig
}

func DefaultCacheSettings() CacheSettings {
	f, p := cachefile.DefaultConfig(""), cachepg.DefaultConfig()
	return CacheSettings{Driver: MemoryCache, Config: cache.DefaultConfig(cache.Namespace{}), Memory: memory.DefaultConfig(), File: FileCacheSettings{MaxEntries: f.MaxEntries, MaxBytes: f.MaxBytes, Sync: f.Sync}, Postgres: PostgresCacheSettings{Schema: p.Schema, MaxEntries: p.MaxEntries, MaxBytes: p.MaxBytes}, Leases: lease.DefaultConfig(cache.Namespace{}), Coordination: cache.DefaultCoordinationConfig()}
}

type CacheStores map[cache.StoreName]CacheSettings

func (m *CacheStores) UnmarshalText(data []byte) error {
	schema, err := CacheSettingsConfigSchema()
	if err != nil {
		return err
	}
	values, err := config.DecodeTable(string(data), schema, func(cache.StoreName) CacheSettings { return DefaultCacheSettings() }, nil)
	if err != nil {
		return err
	}
	*m = values
	return nil
}

type CachesSettings struct {
	Default cache.StoreName
	Stores  CacheStores
}

func DefaultCachesSettings() CachesSettings { return CachesSettings{Default: "default"} }
