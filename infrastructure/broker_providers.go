package infrastructure

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/pubsub"
	"github.com/weiloon1234/Foundry-Go/pubsub/memory"
	"github.com/weiloon1234/Foundry-Go/redis"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

func (p *Plan) broker(name pubsub.ConnectionName, s BrokerSettings) {
	owner := foundation.ProviderID(string(BrokerProvider(name)) + ".backend")
	key := foundation.NewKey[*ownedAdapter[pubsub.Backend]](string(owner))
	var requires []foundation.ProviderID
	if s.Driver == RedisBroker {
		requires = append(requires, RedisProvider(s.Redis))
	}
	p.providers = append(p.providers, adapterModule(owner, key, requires, func(r foundation.Resolver) (*ownedAdapter[pubsub.Backend], error) {
		if s.Driver == RedisBroker {
			client, err := foundation.Resolve(r, RedisKey(s.Redis))
			if err != nil {
				return nil, err
			}
			return &ownedAdapter[pubsub.Backend]{value: client}, nil
		}
		backend, err := memory.New(s.Config.MaxSubscriptions)
		if err != nil {
			return nil, err
		}
		return &ownedAdapter[pubsub.Backend]{value: backend, close: func(context.Context) error { return backend.Close() }}, nil
	}))
	p.providers = append(p.providers, pubsub.Module(BrokerProvider(name), BrokerKey(name), s.Config, []foundation.ProviderID{owner}, func(r foundation.Resolver) (pubsub.Backend, error) {
		adapter, err := foundation.Resolve(r, key)
		if err != nil {
			return nil, err
		}
		return adapter.value, nil
	}))
}
func (p *Plan) realtime(name websocket.ConnectionName, s RealtimeConnectionSettings) {
	var requires []foundation.ProviderID
	if s.Driver == RedisRealtime {
		requires = append(requires, RedisProvider(s.Redis))
	}
	p.providers = append(p.providers, foundation.Module{Name: RealtimeProvider(name), Requires: requires, OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, RealtimeKey(name), func(r foundation.Resolver) (*websocket.BackendConnection, error) {
			if s.Driver == LocalRealtime {
				return websocket.NewLocalConnection(), nil
			}
			client, err := foundation.Resolve(r, RedisKey(s.Redis))
			if err != nil {
				return nil, err
			}
			backend, err := redis.NewWebSocketBackend(client)
			if err != nil {
				return nil, err
			}
			return websocket.NewClusterConnection(backend, s.Cluster)
		})
	}})
}
