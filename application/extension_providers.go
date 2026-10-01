package application

import (
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/extensions/slots"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/settings"
	"github.com/weiloon1234/Foundry-Go/translations"
)

func registerExtensions(builder *foundation.Builder, s FeatureSettings, source clock.Clock, models []slots.Declaration, pools []database.ConnectionName) {
	c := s.Extensions
	if s.ExtensionCleanup.Jobs != "" {
		registerExtensionCleanup(builder, s, pools)
	}
	if len(models) > 0 {
		// A model can be deleted through any configured pool. On the extension
		// store's pool, cleanup joins the deletion transaction; on another it
		// runs after commit. Each configured connection is one pool, so every
		// pool receives each observer once. Observer registration must happen
		// here, before the pools bind their frozen observer sets.
		requires := make([]foundation.ProviderID, 0, len(pools))
		for _, name := range pools {
			requires = append(requires, infrastructure.DatabaseProvider(name))
		}
		durable := s.ExtensionCleanup.Jobs != ""
		if durable {
			requires = append(requires, extensionCleanupProvider)
		}
		builder.Register(foundation.Module{Name: "foundry.application.model-extensions", Requires: requires, OnRegister: func(r *foundation.Registrar) error {
			// Only deletion observers receive the cleanup queue: descriptors
			// bound in application jobs never depend on the job dispatcher.
			resolve := func(resolver foundation.Resolver) (slots.Runtime, error) {
				services, err := FromResolver(resolver)
				if err != nil {
					return slots.Runtime{}, err
				}
				runtime, err := services.ModelExtensions()
				if err != nil || !durable {
					return runtime, err
				}
				runtime.CleanupQueue, err = foundation.Resolve(resolver, extensionCleanupKey)
				return runtime, err
			}
			for _, name := range pools {
				for _, declaration := range models {
					if err := declaration.Register(r, infrastructure.DatabaseKey(name), resolve); err != nil {
						return err
					}
				}
			}
			return nil
		}})
	}
	builder.Register(extensions.Module(ExtensionProvider, ExtensionKey, []foundation.ProviderID{FeatureDeclarationsProvider, infrastructure.DatabaseProvider(c.Database)}, func(r foundation.Resolver) (*extensions.Store, error) {
		db, err := foundation.Resolve(r, infrastructure.DatabaseKey(c.Database))
		if err != nil {
			return nil, err
		}
		d, err := foundation.Resolve(r, featureDeclarationsKey)
		if err != nil {
			return nil, err
		}
		registry, err := extensions.NewRegistry(d.Owners...)
		if err != nil {
			return nil, err
		}
		return extensions.New(db, registry, extensions.Config{Schema: c.Schema, Clock: source, MaxActive: c.MaxActive, Timeout: c.Timeout})
	}))
	requires := []foundation.ProviderID{ExtensionProvider}
	if s.Locales.Enabled {
		requires = append(requires, LocaleProvider)
	}
	builder.Register(foundation.Module{Name: "foundry.application.extension-managers", Requires: requires, OnRegister: func(r *foundation.Registrar) error {
		if err := foundation.Factory(r, SettingsKey, func(r foundation.Resolver) (*settings.Manager, error) {
			store, err := foundation.Resolve(r, ExtensionKey)
			if err != nil {
				return nil, err
			}
			d, err := foundation.Resolve(r, featureDeclarationsKey)
			if err != nil {
				return nil, err
			}
			return settings.New(store, d.Settings...)
		}); err != nil {
			return err
		}
		if err := foundation.Factory(r, MetadataKey, func(r foundation.Resolver) (*metadata.Manager, error) {
			store, err := foundation.Resolve(r, ExtensionKey)
			if err != nil {
				return nil, err
			}
			d, err := foundation.Resolve(r, featureDeclarationsKey)
			if err != nil {
				return nil, err
			}
			return metadata.New(store, d.Metadata...)
		}); err != nil {
			return err
		}
		if s.Locales.Enabled {
			return foundation.Factory(r, TranslationKey, func(r foundation.Resolver) (*translations.Manager, error) {
				store, err := foundation.Resolve(r, ExtensionKey)
				if err != nil {
					return nil, err
				}
				d, err := foundation.Resolve(r, featureDeclarationsKey)
				if err != nil {
					return nil, err
				}
				catalog, err := foundation.Resolve(r, LocaleKey)
				if err != nil {
					return nil, err
				}
				return translations.New(store, catalog, d.Translations...)
			})
		}
		return nil
	}})
}
