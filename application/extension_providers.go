package application

import (
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/extensions/slots"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/settings"
	"github.com/weiloon1234/Foundry-Go/translations"
)

func registerExtensions(builder *foundation.Builder, s FeatureSettings, source clock.Clock, models []slots.Declaration) {
	c := s.Extensions
	if len(models) > 0 {
		// Cleanup observers join the owner's deletion transaction, so they
		// belong to the extension store's pool. Observer registration must
		// happen here, before the pool binds its frozen observer set.
		pool := infrastructure.DatabaseKey(c.Database)
		builder.Register(foundation.Module{Name: "foundry.application.model-extensions", Requires: []foundation.ProviderID{infrastructure.DatabaseProvider(c.Database)}, OnRegister: func(r *foundation.Registrar) error {
			resolve := func(resolver foundation.Resolver) (slots.Runtime, error) {
				services, err := FromResolver(resolver)
				if err != nil {
					return slots.Runtime{}, err
				}
				return services.ModelExtensions()
			}
			for _, declaration := range models {
				if err := declaration.Register(r, pool, resolve); err != nil {
					return err
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
