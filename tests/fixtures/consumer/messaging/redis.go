package messaging

import (
	"foundry.test/consumer/caching"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/pubsub"
)

var Broker = foundation.NewKey[*pubsub.Broker]("member-publications")

// RedisModule keeps subscriptions alive only while their borrowed connection is owned.
func RedisModule(config pubsub.Config, redisProvider foundation.ProviderID) foundation.Module {
	return pubsub.Module("member-publications", Broker, config, []foundation.ProviderID{redisProvider}, func(r foundation.Resolver) (pubsub.Backend, error) {
		return foundation.Resolve(r, caching.RedisConnection)
	})
}
