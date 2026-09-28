package infrastructure

import (
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/pubsub"
	"github.com/weiloon1234/Foundry-Go/redis"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

const Provider foundation.ProviderID = "foundry.infrastructure"

var ServicesKey = foundation.NewKey[*Services](string(Provider))

// Services supplies typed constructor dependencies. Disabled families have no
// registry. Default access is automatic and errors if that family is disabled.
type Services struct {
	Databases    *database.Connections
	Redis        *redis.Connections
	Storage      *storage.Registry
	Caches       *cache.Stores
	Mailers      *email.Mailers
	Jobs         *jobs.Connections
	HTTPClients  *httpclient.Clients
	Brokers      *pubsub.Brokers
	Realtime     *websocket.Connections
	Coordination *lease.Manager
}

func (s *Services) Database() (*database.DB, error) {
	if s == nil {
		return (*database.Connections)(nil).Default()
	}
	return s.Databases.Default()
}
func (s *Services) Cache() (*cache.Store, error) {
	if s == nil {
		return (*cache.Stores)(nil).Default()
	}
	return s.Caches.Default()
}
func (s *Services) Disk() (*storage.Disk, error) {
	if s == nil {
		return (*storage.Registry)(nil).Default()
	}
	return s.Storage.Default()
}
func (s *Services) RedisConnection() (*redis.Client, error) {
	if s == nil {
		return (*redis.Connections)(nil).Default()
	}
	return s.Redis.Default()
}
func DatabaseProvider(name database.ConnectionName) foundation.ProviderID {
	return foundation.ProviderID("foundry.infrastructure.database." + string(name))
}
func DatabaseKey(name database.ConnectionName) foundation.Key[*database.DB] {
	return foundation.NewKey[*database.DB](string(DatabaseProvider(name)))
}
func RedisProvider(name redis.ConnectionName) foundation.ProviderID {
	return foundation.ProviderID("foundry.infrastructure.redis." + string(name))
}
func RedisKey(name redis.ConnectionName) foundation.Key[*redis.Client] {
	return foundation.NewKey[*redis.Client](string(RedisProvider(name)))
}
func DiskProvider(name storage.DiskID) foundation.ProviderID {
	return foundation.ProviderID("foundry.infrastructure.disk." + string(name))
}
func DiskKey(name storage.DiskID) foundation.Key[*storage.Disk] {
	return foundation.NewKey[*storage.Disk](string(DiskProvider(name)))
}
func CacheProvider(name cache.StoreName) foundation.ProviderID {
	return foundation.ProviderID("foundry.infrastructure.cache." + string(name))
}
func CacheKey(name cache.StoreName) foundation.Key[*cache.Store] {
	return foundation.NewKey[*cache.Store](string(CacheProvider(name)))
}
func CredentialProvider(name credentials.Name) foundation.ProviderID {
	return foundation.ProviderID("foundry.infrastructure.credentials." + string(name))
}
func CredentialKey(name credentials.Name) foundation.Key[credentials.Provider] {
	return foundation.NewKey[credentials.Provider](string(CredentialProvider(name)))
}
