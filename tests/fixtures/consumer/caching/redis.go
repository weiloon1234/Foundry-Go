package caching

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/redis"
)

// RedisConnection is registered once; Foundry owns startup and reverse cleanup.
var RedisConnection = foundation.NewKey[*redis.Client]("profiles.redis")

func RedisModule(config redis.Config) foundation.Module {
	return redis.Module("profiles.redis", RedisConnection, config)
}

// BindRedis changes infrastructure without changing the typed model key, payload,
// getters, loader or query-facing domain code in profiles.go.
func BindRedis(client *redis.Client, namespace cache.Namespace) (Profiles, error) {
	store, err := RedisStore(client, namespace)
	if err != nil {
		return Profiles{}, err
	}
	return Bind(store)
}

// RedisHealth composes the framework connection check into application readiness.
func RedisHealth(ctx context.Context, client *redis.Client) error { return client.Ping(ctx) }

// RedisStore binds all profile values and typed invalidation descriptors to the
// same application namespace; the application module retains connection ownership.
func RedisStore(client *redis.Client, namespace cache.Namespace) (*cache.Store, error) {
	return cache.NewStore(client, cache.DefaultConfig(namespace))
}
