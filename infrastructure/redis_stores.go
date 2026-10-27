package infrastructure

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/namedservice"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	"github.com/weiloon1234/Foundry-Go/redis"
)

// RedisStore is a sealed application store declaration. Its typed constructor
// owns both runtime construction and inventory metadata. A zero namespace uses
// the application namespace; an empty connection selects the Redis default.
type RedisStore interface {
	resolve(*Plan) (resolvedRedisStore, error)
}

type resolvedRedisStore struct {
	connection redis.ConnectionName
	namespace  keyspace.Namespace
	feature    keyspace.Feature
	key        string
	module     foundation.Module
}

type redisStore[T any] struct {
	key        foundation.Key[T]
	connection redis.ConnectionName
	namespace  keyspace.Namespace
	feature    keyspace.Feature
	validate   func(keyspace.Namespace) error
	build      func(*redis.Client, keyspace.Namespace) (T, error)
}

// RedisRateLimitStore declares a borrowing rate-limit store under a typed service
// key. The key name follows named-service syntax. Resolve it during bootstrap,
// then bind ordinary ratelimit declarations to the resulting store.
func RedisRateLimitStore(key foundation.Key[*ratelimit.Store], connection redis.ConnectionName, config ratelimit.Config) RedisStore {
	return redisStore[*ratelimit.Store]{key: key, connection: connection, namespace: config.Namespace, feature: ratelimit.KeyFeature,
		validate: func(namespace keyspace.Namespace) error { c := config; c.Namespace = namespace; return c.Validate() },
		build: func(client *redis.Client, namespace keyspace.Namespace) (*ratelimit.Store, error) {
			c := config
			c.Namespace = namespace
			return ratelimit.NewStore(client, c)
		},
	}
}

// RedisLockoutStore declares a borrowing credential-protection store. Its config
// and connection are shared by runtime construction and service-free inspection.
func RedisLockoutStore(key foundation.Key[*lockout.Store], connection redis.ConnectionName, config lockout.Config) RedisStore {
	return redisStore[*lockout.Store]{key: key, connection: connection, namespace: config.Namespace, feature: lockout.KeyFeature,
		validate: func(namespace keyspace.Namespace) error { c := config; c.Namespace = namespace; return c.Validate() },
		build: func(client *redis.Client, namespace keyspace.Namespace) (*lockout.Store, error) {
			c := config
			c.Namespace = namespace
			return lockout.NewStore(client, c)
		},
	}
}

// WithRedisStores snapshots application declarations before plan resolution.
func WithRedisStores(stores ...RedisStore) Option {
	owned := slices.Clone(stores)
	return func(o *options) error {
		if len(owned) > namedservice.MaxEntries-len(o.redisStores) {
			return safeerror.NewFault(fault.Invalid, "too many declared Redis stores")
		}
		for _, store := range owned {
			if store == nil {
				return safeerror.NewFault(fault.Invalid, "nil Redis store declaration")
			}
		}
		o.redisStores = append(o.redisStores, owned...)
		return nil
	}
}

func (d redisStore[T]) resolve(p *Plan) (resolvedRedisStore, error) {
	if err := namedservice.Validate(d.key.Name()); err != nil {
		return resolvedRedisStore{}, err
	}
	connection, err := p.redisReference(d.connection)
	if err != nil {
		return resolvedRedisStore{}, err
	}
	namespace := d.namespace
	if namespace == (keyspace.Namespace{}) {
		namespace = p.settings.Namespace
	}
	if err := d.validate(namespace); err != nil {
		return resolvedRedisStore{}, err
	}
	module := foundation.Module{Name: foundation.ProviderID("foundry.infrastructure.redisstore." + d.key.Name()), Requires: []foundation.ProviderID{RedisProvider(connection)}, OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, d.key, func(r foundation.Resolver) (T, error) {
			client, err := foundation.Resolve(r, RedisKey(connection))
			if err != nil {
				var zero T
				return zero, err
			}
			return d.build(client, namespace)
		})
	}}
	return resolvedRedisStore{connection: connection, namespace: namespace, feature: d.feature, key: d.key.Name(), module: module}, nil
}

func (p *Plan) resolveRedisStores() error {
	seen := make(map[string]bool)
	for _, declaration := range p.options.redisStores {
		store, err := declaration.resolve(p)
		if err != nil {
			return err
		}
		if seen[store.key] {
			return safeerror.NewFault(fault.Duplicate, "Redis store service key is duplicated")
		}
		seen[store.key] = true
		p.redisStores = append(p.redisStores, store)
	}
	return nil
}
