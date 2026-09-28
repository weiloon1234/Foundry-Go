package coordination

import (
	"foundry.test/consumer/caching"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/lease"
)

var Manager = foundation.NewKey[*lease.Manager]("member-leases")

// RedisModule depends explicitly on the existing connection provider. Reverse
// shutdown drains leases before that provider closes its client.
func RedisModule(config lease.Config, redisProvider foundation.ProviderID) foundation.Module {
	return lease.Module("member-leases", Manager, config, []foundation.ProviderID{redisProvider}, func(r foundation.Resolver) (lease.Backend, error) {
		return foundation.Resolve(r, caching.RedisConnection)
	})
}
