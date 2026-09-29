package infrastructure

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/inline"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/redis"
	"slices"
)

func (p *Plan) jobConnection(name jobs.ConnectionName, s JobConnectionSettings) {
	owner := foundation.ProviderID(string(JobProvider(name)) + ".backend")
	key := foundation.NewKey[*ownedAdapter[jobs.Backend]](string(owner))
	var requires []foundation.ProviderID
	if s.Driver == RedisJobs {
		requires = append(requires, RedisProvider(s.Redis))
	}
	p.providers = append(p.providers, adapterModule(owner, key, requires, func(r foundation.Resolver) (*ownedAdapter[jobs.Backend], error) {
		if s.Driver == RedisJobs {
			client, err := foundation.Resolve(r, RedisKey(s.Redis))
			if err != nil {
				return nil, err
			}
			backend, err := redis.NewJobBackend(client, s.Queue)
			if err != nil {
				return nil, err
			}
			return &ownedAdapter[jobs.Backend]{value: backend}, nil
		}
		if s.Driver == SyncJobs {
			backend, err := inline.New(memory.Config{QueueConfig: s.Queue, Clock: p.options.clock})
			if err != nil {
				return nil, err
			}
			return &ownedAdapter[jobs.Backend]{value: backend, close: func(context.Context) error { return backend.Close() }}, nil
		}
		backend, err := memory.New(memory.Config{QueueConfig: s.Queue, Clock: p.options.clock})
		if err != nil {
			return nil, err
		}
		return &ownedAdapter[jobs.Backend]{value: backend, close: func(context.Context) error { return backend.Close() }}, nil
	}))
	module := jobs.ConnectionModule(JobProvider(name), JobDispatcherKey(name), s.Dispatch, []foundation.ProviderID{owner}, func(r foundation.Resolver) (jobs.Backend, []jobs.Declaration, error) {
		backend, err := foundation.Resolve(r, key)
		if err != nil {
			return nil, nil, err
		}
		return backend.value, nil, nil
	})
	register := module.OnRegister
	module.OnRegister = func(r *foundation.Registrar) error {
		if err := register(r); err != nil {
			return err
		}
		return foundation.Factory(r, JobKey(name), func(r foundation.Resolver) (*jobs.Connection, error) {
			dispatcher, err := foundation.Resolve(r, JobDispatcherKey(name))
			if err != nil {
				return nil, err
			}
			return jobs.NewConnection(dispatcher, s.DefaultQueue)
		})
	}
	p.providers = append(p.providers, module)
}

// Worker selects one configured connection without duplicating its namespace or
// default queue. It registers no resources until the returned module is added.
func (p *Plan) Worker(owner foundation.ProviderID, name jobs.ConnectionName, config jobs.WorkerConfig, requires []foundation.ProviderID) (foundation.Module, error) {
	if p == nil {
		return foundation.Module{}, fault.New(fault.Invalid, "worker requires an infrastructure plan")
	}
	if name == "" {
		name = p.settings.Jobs.Default
	}
	connection, ok := p.settings.Jobs.Connections[name]
	if !ok {
		return foundation.Module{}, fault.New(fault.Missing, "worker connection is not configured")
	}
	if config.Namespace == (keyspace.Namespace{}) {
		config.Namespace = connection.Dispatch.Namespace
	}
	if config.Namespace != connection.Dispatch.Namespace {
		return foundation.Module{}, fault.New(fault.Invalid, "worker namespace differs from its connection")
	}
	if len(config.Queues) == 0 {
		config.Queues = []jobs.Subscription{{Queue: connection.DefaultQueue, Weight: 1}}
	}
	if err := config.Validate(); err != nil {
		return foundation.Module{}, err
	}
	requires = append(slices.Clone(requires), JobProvider(name))
	return jobs.WorkerModule(owner, JobDispatcherKey(name), config, requires), nil
}
