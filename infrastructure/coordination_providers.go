package infrastructure

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/lease/memory"
)

func (p *Plan) coordination(s CoordinationSettings) {
	owner := foundation.ProviderID(string(CoordinationProvider) + ".backend")
	key := foundation.NewKey[*ownedAdapter[lease.Backend]](string(owner))
	var requires []foundation.ProviderID
	if s.Driver == RedisCoordination {
		requires = append(requires, RedisProvider(s.Redis))
	}
	p.providers = append(p.providers, adapterModule(owner, key, requires, func(r foundation.Resolver) (*ownedAdapter[lease.Backend], error) {
		if s.Driver == RedisCoordination {
			client, err := foundation.Resolve(r, RedisKey(s.Redis))
			if err != nil {
				return nil, err
			}
			return &ownedAdapter[lease.Backend]{value: client}, nil
		}
		backend, err := memory.New(s.MaxEntries)
		if err != nil {
			return nil, err
		}
		return &ownedAdapter[lease.Backend]{value: backend, close: func(context.Context) error { return backend.Close() }}, nil
	}))
	p.providers = append(p.providers, lease.Module(CoordinationProvider, CoordinationKey, s.Config, []foundation.ProviderID{owner}, func(r foundation.Resolver) (lease.Backend, error) {
		adapter, err := foundation.Resolve(r, key)
		if err != nil {
			return nil, err
		}
		return adapter.value, nil
	}))
}
