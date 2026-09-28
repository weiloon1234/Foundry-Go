package infrastructure

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/redis"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
	"github.com/weiloon1234/Foundry-Go/storage/s3"
)

func (p *Plan) assemble() {
	s := p.settings
	for _, name := range keys(s.Credentials) {
		settings := s.Credentials[name]
		id, key := CredentialProvider(name), CredentialKey(name)
		owned := foundation.NewKey[*credentials.Source](string(id) + ".source")
		p.providers = append(p.providers, foundation.Module{Name: id, OnRegister: func(r *foundation.Registrar) error {
			if err := foundation.Factory(r, owned, func(foundation.Resolver) (*credentials.Source, error) { return credentials.Prepare(settings) }); err != nil {
				return err
			}
			return foundation.Factory(r, key, func(r foundation.Resolver) (credentials.Provider, error) { return foundation.Resolve(r, owned) })
		}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
			source, err := foundation.Resolve(r.Services(), owned)
			if err != nil {
				return err
			}
			if err = r.OnShutdown("credentials", func(context.Context) error { return source.Close() }); err != nil {
				return errors.Join(err, source.Close())
			}
			return source.Start(ctx)
		}})
	}
	for _, name := range keys(p.options.credentials) {
		provider := p.options.credentials[name]
		key := CredentialKey(name)
		p.providers = append(p.providers, foundation.Module{Name: CredentialProvider(name), OnRegister: func(r *foundation.Registrar) error { return foundation.Provide(r, key, provider) }})
	}
	for _, name := range keys(s.Database.Connections) {
		p.providers = append(p.providers, postgres.RoutedModule(DatabaseProvider(name), DatabaseKey(name), s.Database.Connections[name].Config()))
	}
	for _, name := range keys(s.Redis.Connections) {
		p.providers = append(p.providers, redis.Module(RedisProvider(name), RedisKey(name), s.Redis.Connections[name].Config()))
	}
	for _, name := range keys(s.Storage.Disks) {
		p.disk(name, s.Storage.Disks[name])
	}
	for _, name := range keys(s.Cache.Stores) {
		p.cache(name, s.Cache.Stores[name])
	}
	p.storageRegistry()
	p.supporting()
	requires := make([]foundation.ProviderID, 0, len(p.providers))
	for _, provider := range p.providers {
		requires = append(requires, provider.ID())
	}
	p.providers = append(p.providers, foundation.Module{Name: Provider, Requires: requires, OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, ServicesKey, func(r foundation.Resolver) (*Services, error) {
			services := &Services{}
			if len(s.Database.Connections) > 0 {
				var entries []database.Connection
				for _, name := range keys(s.Database.Connections) {
					db, err := foundation.Resolve(r, DatabaseKey(name))
					if err != nil {
						return nil, err
					}
					entries = append(entries, database.Connection{Name: name, Value: db})
				}
				var err error
				services.Databases, err = database.NewConnections(s.Database.Default, entries...)
				if err != nil {
					return nil, err
				}
			}
			if len(s.Redis.Connections) > 0 {
				var entries []redis.Connection
				for _, name := range keys(s.Redis.Connections) {
					client, err := foundation.Resolve(r, RedisKey(name))
					if err != nil {
						return nil, err
					}
					entries = append(entries, redis.Connection{Name: name, Value: client})
				}
				var err error
				services.Redis, err = redis.NewConnections(s.Redis.Default, entries...)
				if err != nil {
					return nil, err
				}
			}
			if len(s.Storage.Disks) > 0 {
				var err error
				services.Storage, err = foundation.Resolve(r, storageRegistryKey)
				if err != nil {
					return nil, err
				}
			}
			if len(s.Cache.Stores) > 0 {
				var entries []cache.NamedStore
				for _, name := range keys(s.Cache.Stores) {
					store, err := foundation.Resolve(r, CacheKey(name))
					if err != nil {
						return nil, err
					}
					entries = append(entries, cache.NamedStore{Name: name, Value: store})
				}
				var err error
				services.Caches, err = cache.NewStores(s.Cache.Default, entries...)
				if err != nil {
					return nil, err
				}
			}
			if err := resolveSupporting(r, s, services); err != nil {
				return nil, err
			}
			return services, nil
		})
	}})
}
func (p *Plan) disk(name storage.DiskID, settings DiskSettings) {
	owner := foundation.ProviderID(string(DiskProvider(name)) + ".backend")
	var factory func(foundation.Resolver) (storage.Backend, error)
	if settings.Driver == LocalDisk {
		config := local.DefaultConfig(settings.Local.Root)
		config.MaxObjectBytes = settings.Config.MaxObjectBytes
		config.MaxScan = settings.Local.MaxScan
		config.Sync = settings.Local.Sync
		config.Clock = p.options.clock
		key := foundation.NewKey[*local.Backend](string(owner))
		p.providers = append(p.providers, local.Module(owner, key, config))
		factory = func(r foundation.Resolver) (storage.Backend, error) { return foundation.Resolve(r, key) }
	} else {
		key := foundation.NewKey[*s3.Backend](string(owner))
		// Validation already checked the concrete configuration; the placeholder
		// keeps explicit R2 validation intact until the provider is resolved at Build.
		placeholder := credentials.ProviderFunc(func(context.Context) (credentials.Value, error) { return credentials.Value{}, nil })
		config, _ := settings.cloudConfig(placeholder)
		if settings.Cloud.Credentials == "" {
			config.Credentials = nil
			p.providers = append(p.providers, s3.Module(owner, key, config))
		} else {
			source := settings.Cloud.Credentials
			p.providers = append(p.providers, s3.CredentialsModule(owner, key, config, []foundation.ProviderID{CredentialProvider(source)}, func(r foundation.Resolver) (credentials.Provider, error) {
				return foundation.Resolve(r, CredentialKey(source))
			}))
		}
		factory = func(r foundation.Resolver) (storage.Backend, error) { return foundation.Resolve(r, key) }
	}
	p.providers = append(p.providers, storage.Module(DiskProvider(name), DiskKey(name), storage.DefineDisk(name), settings.Config, []foundation.ProviderID{owner}, factory))
}
