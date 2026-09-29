package infrastructure

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/cache"
	cachefile "github.com/weiloon1234/Foundry-Go/cache/file"
	"github.com/weiloon1234/Foundry-Go/cache/memory"
	cachenull "github.com/weiloon1234/Foundry-Go/cache/null"
	cachepg "github.com/weiloon1234/Foundry-Go/cache/postgres"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/lease"
)

func (p *Plan) cache(name cache.StoreName, settings CacheSettings) {
	owner := foundation.ProviderID(string(CacheProvider(name)) + ".backend")
	key := foundation.NewKey[*ownedAdapter[cache.Backend]](string(owner))
	var dependencies []foundation.ProviderID
	switch settings.Driver {
	case RedisCache:
		dependencies = append(dependencies, RedisProvider(settings.Redis))
	case PostgresCache:
		dependencies = append(dependencies, DatabaseProvider(settings.Database))
	}
	p.providers = append(p.providers, adapterModule(owner, key, dependencies, func(r foundation.Resolver) (*ownedAdapter[cache.Backend], error) {
		result := &ownedAdapter[cache.Backend]{}
		switch settings.Driver {
		case NullCache:
			result.value = cachenull.New()
		case MemoryCache:
			backend, err := memory.New(settings.Memory, p.options.clock)
			if err != nil {
				return nil, err
			}
			result.value = backend
			result.close = backend.Close
		case FileCache:
			backend, err := cachefile.Prepare(settings.fileConfig(p.options.clock, p.options.logger))
			if err != nil {
				return nil, err
			}
			result.value = backend
			result.start = backend.Start
			result.close = func(ctx context.Context) error {
				err := backend.Close(ctx)
				<-backend.Done()
				return errors.Join(err, backend.Close(context.Background()))
			}
		case PostgresCache:
			db, err := foundation.Resolve(r, DatabaseKey(settings.Database))
			if err != nil {
				return nil, err
			}
			backend, err := cachepg.New(db, settings.postgresConfig(p.options.clock, p.options.logger))
			if err != nil {
				return nil, err
			}
			// Start only launches the owned pruner; Close drains it before the
			// database provider shuts down.
			result.value = backend
			result.start = backend.Start
			result.close = func(ctx context.Context) error {
				err := backend.Close(ctx)
				<-backend.Done()
				return errors.Join(err, backend.Close(context.Background()))
			}
		case RedisCache:
			client, err := foundation.Resolve(r, RedisKey(settings.Redis))
			if err != nil {
				return nil, err
			}
			result.value = client
		}
		return result, nil
	}))
	requires := []foundation.ProviderID{owner}
	manager := foundation.NewKey[*lease.Manager](string(CacheProvider(name)) + ".leases")
	if settings.Require.DistributedFills {
		leaseOwner := foundation.ProviderID(manager.Name())
		requires = append(requires, leaseOwner)
		p.providers = append(p.providers, lease.Module(leaseOwner, manager, settings.Leases, []foundation.ProviderID{RedisProvider(settings.Redis)}, func(r foundation.Resolver) (lease.Backend, error) {
			return foundation.Resolve(r, RedisKey(settings.Redis))
		}))
	}
	p.providers = append(p.providers, foundation.Module{Name: CacheProvider(name), Requires: requires, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		// The store drains its running fills (detached Remember loaders and
		// Flexible refreshes) before the backend and lease providers close.
		store, err := foundation.Resolve(r.Services(), CacheKey(name))
		if err != nil {
			return err
		}
		if err := r.OnShutdown("store", store.Close); err != nil {
			return errors.Join(err, store.Close(ctx))
		}
		return nil
	}, OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, CacheKey(name), func(r foundation.Resolver) (*cache.Store, error) {
			var store *cache.Store
			var err error
			if settings.Require.DistributedFills {
				m, e := foundation.Resolve(r, manager)
				if e != nil {
					return nil, e
				}
				store, err = cache.NewCoordinatedStore(m, settings.Config, settings.Coordination, cache.WithLogger(p.options.logger))
			} else {
				b, e := foundation.Resolve(r, key)
				if e != nil {
					return nil, e
				}
				store, err = cache.NewStore(b.value, settings.Config, cache.WithLogger(p.options.logger))
			}
			if err != nil {
				return nil, err
			}
			if err = store.Require(settings.Require); err != nil {
				return nil, err
			}
			return store, nil
		})
	}})
}

// MigrationTarget contributes definitions to an explicit migration command. It
// never runs during normal application boot. Multiple stores sharing one target
// return one contribution; the existing migration registry owns deduplication.
type MigrationTarget struct {
	Connection  database.ConnectionName
	Schema      string
	Definitions []migrate.Definition
}

func (p *Plan) Migrations() []MigrationTarget {
	if p == nil {
		return nil
	}
	var result []MigrationTarget
	seen := make(map[struct {
		connection database.ConnectionName
		schema     string
	}]bool)
	for _, t := range cacheTargets(p.settings) {
		key := struct {
			connection database.ConnectionName
			schema     string
		}{t.Connection, t.Schema}
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, t.MigrationTarget)
	}
	return result
}
