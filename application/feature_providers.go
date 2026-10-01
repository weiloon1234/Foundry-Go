package application

import (
	"context"
	"maps"
	"slices"

	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/extensions/slots"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/health"
	"github.com/weiloon1234/Foundry-Go/health/checks"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/notifications"
	"github.com/weiloon1234/Foundry-Go/settings"
	"github.com/weiloon1234/Foundry-Go/translations"
	"github.com/weiloon1234/Foundry-Go/validation"
)

const ExtensionProvider foundation.ProviderID = "foundry.application.extensions"
const NotificationProvider foundation.ProviderID = "foundry.application.notifications"
const AttachmentProvider foundation.ProviderID = "foundry.application.attachments"
const ReportProvider foundation.ProviderID = "foundry.application.reports"
const LocaleProvider foundation.ProviderID = "foundry.application.locales"
const HealthProvider foundation.ProviderID = "foundry.application.health"
const AuditProvider foundation.ProviderID = "foundry.application.audit"

var ExtensionKey = foundation.NewKey[*extensions.Store](string(ExtensionProvider))
var SettingsKey = foundation.NewKey[*settings.Manager]("foundry.application.settings")
var MetadataKey = foundation.NewKey[*metadata.Manager]("foundry.application.metadata")
var TranslationKey = foundation.NewKey[*translations.Manager]("foundry.application.translations")
var NotificationKey = foundation.NewKey[*notifications.Manager](string(NotificationProvider))
var AttachmentKey = foundation.NewKey[*attachments.Manager](string(AttachmentProvider))
var ReportKey = foundation.NewKey[*datatable.Manager](string(ReportProvider))
var LocaleKey = foundation.NewKey[*i18n.Catalog](string(LocaleProvider))
var HealthKey = foundation.NewKey[*health.Registry](string(HealthProvider))
var AuditKey = foundation.NewKey[*audit.Recorder](string(AuditProvider))

func registerFeatures(ctx context.Context, builder *foundation.Builder, settings Settings, source clock.Clock, constructors []Features, models []slots.Declaration, keys *encryption.Keyring) error {
	if len(constructors) > 128 {
		return fault.New(fault.Invalid, "too many feature declaration constructors")
	}
	s := settings.Features
	builder.Register(foundation.Module{Name: FeatureDeclarationsProvider, Requires: []foundation.ProviderID{Provider}, OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, featureDeclarationsKey, func(r foundation.Resolver) (FeatureDeclarations, error) {
			return mergeFeatures(r, s, constructors, models)
		})
	}})
	registerEncryption(builder, keys)
	registerAuth(builder, s.Auth, source)
	registerIdempotency(builder, s.Idempotency, settings.Services.Namespace)
	if s.Locales.Enabled {
		builder.Register(foundation.Module{Name: LocaleProvider, Requires: []foundation.ProviderID{FeatureDeclarationsProvider}, OnRegister: func(r *foundation.Registrar) error {
			return foundation.Factory(r, LocaleKey, func(r foundation.Resolver) (*i18n.Catalog, error) {
				d, err := foundation.Resolve(r, featureDeclarationsKey)
				if err != nil {
					return nil, err
				}
				locales, err := i18n.NewLocaleSet(s.Locales.Default, s.Locales.Locales...)
				if err != nil {
					return nil, err
				}
				definitions := append(validation.MessageDefinitions(), http.MessageDefinitions()...)
				definitions = append(definitions, d.Messages...)
				return i18n.NewCatalog(ctx, locales, i18n.CatalogOptions{Fallback: s.Locales.Fallback}, definitions, d.Catalog)
			})
		}})
	}
	if s.Extensions.Enabled {
		registerExtensions(builder, s, source, models, slices.Sorted(maps.Keys(settings.Services.Database.Connections)))
	}
	if s.Notifications.Enabled {
		c := s.Notifications
		builder.Register(notifications.Module(NotificationProvider, NotificationKey, []foundation.ProviderID{FeatureDeclarationsProvider, infrastructure.DatabaseProvider(c.Database)}, func(r foundation.Resolver) (*notifications.Manager, error) {
			db, err := foundation.Resolve(r, infrastructure.DatabaseKey(c.Database))
			if err != nil {
				return nil, err
			}
			d, err := foundation.Resolve(r, featureDeclarationsKey)
			if err != nil {
				return nil, err
			}
			registry, err := notifications.NewRegistry(d.Notifications...)
			if err != nil {
				return nil, err
			}
			return notifications.New(db, registry, notifications.Config{Schema: c.Schema, Clock: source, MaxActive: c.MaxActive, Timeout: c.Timeout})
		}))
	}
	if s.Audit.Enabled {
		builder.Register(foundation.Module{Name: AuditProvider, Requires: []foundation.ProviderID{infrastructure.DatabaseProvider(s.Audit.Database)}, OnRegister: func(r *foundation.Registrar) error {
			if err := foundation.Factory(r, AuditKey, func(foundation.Resolver) (*audit.Recorder, error) { return audit.New(s.Audit.Config) }); err != nil {
				return err
			}
			return foundation.Factory(r, AuditScopeKey, func(resolver foundation.Resolver) (*audit.Scope, error) {
				recorder, err := foundation.Resolve(resolver, AuditKey)
				if err != nil {
					return nil, err
				}
				db, err := foundation.Resolve(resolver, infrastructure.DatabaseKey(s.Audit.Database))
				if err != nil {
					return nil, err
				}
				return audit.NewScope(db, s.Audit.Schema, recorder)
			})
		}})
	}
	if s.Attachments.Enabled {
		requires := []foundation.ProviderID{FeatureDeclarationsProvider, ExtensionProvider, Provider}
		if s.Locales.Enabled {
			requires = append(requires, LocaleProvider)
		}
		builder.Register(attachments.Module(AttachmentProvider, AttachmentKey, requires, func(r foundation.Resolver) (*attachments.Manager, error) {
			store, err := foundation.Resolve(r, ExtensionKey)
			if err != nil {
				return nil, err
			}
			services, err := FromResolver(r)
			if err != nil {
				return nil, err
			}
			d, err := foundation.Resolve(r, featureDeclarationsKey)
			if err != nil {
				return nil, err
			}
			dependencies := attachments.Dependencies{Store: store, Disks: services.Storage, Image: services.image}
			if s.Locales.Enabled {
				dependencies.Locales, err = foundation.Resolve(r, LocaleKey)
				if err != nil {
					return nil, err
				}
			}
			return attachments.New(dependencies, s.Attachments.Config, d.Attachments...)
		}))
	}
	if s.Reports.Enabled {
		c := s.Reports
		requires := []foundation.ProviderID{FeatureDeclarationsProvider, infrastructure.DatabaseProvider(c.Database)}
		if s.Locales.Enabled {
			requires = append(requires, LocaleProvider)
		}
		builder.Register(datatable.Module(ReportProvider, ReportKey, requires, func(r foundation.Resolver) (*datatable.Manager, error) {
			db, err := foundation.Resolve(r, infrastructure.DatabaseKey(c.Database))
			if err != nil {
				return nil, err
			}
			d, err := foundation.Resolve(r, featureDeclarationsKey)
			if err != nil {
				return nil, err
			}
			dependencies := datatable.Dependencies{Database: db}
			if s.Locales.Enabled {
				catalog, err := foundation.Resolve(r, LocaleKey)
				if err != nil {
					return nil, err
				}
				dependencies.Locales = catalog
				dependencies.Labels = catalog.Label
			}
			return datatable.New(dependencies, c.Config, d.Reports...)
		}))
	}
	if s.Health.Enabled {
		requires := []foundation.ProviderID{FeatureDeclarationsProvider, Provider}
		realtime := settings.Realtime.Enabled && s.Health.ConfiguredConnections
		if realtime {
			requires = append(requires, RealtimeProvider)
		}
		builder.Register(health.Module(HealthProvider, HealthKey, s.Health.Config, requires, func(r foundation.Resolver) ([]health.Probe, error) {
			d, err := foundation.Resolve(r, featureDeclarationsKey)
			if err != nil {
				return nil, err
			}
			services, err := FromResolver(r)
			if err != nil {
				return nil, err
			}
			probes := d.Readiness
			if s.Health.ConfiguredConnections {
				if services.Databases != nil {
					for _, name := range services.Databases.Names() {
						db, err := services.Databases.Connection(name)
						if err != nil {
							return nil, err
						}
						probes = append(probes, db.ReadinessProbes(health.ProbeID("database."+string(name)+".primary"), health.ProbeID("database."+string(name)+".read"))...)
					}
				}
				if services.Redis != nil {
					for _, name := range services.Redis.Names() {
						client, err := services.Redis.Connection(name)
						if err != nil {
							return nil, err
						}
						probes = append(probes, health.Probe{ID: health.ProbeID("redis." + string(name)), Check: client.Ping})
					}
				}
				if realtime {
					hub, err := services.RealtimeHub()
					if err != nil {
						return nil, err
					}
					probes = append(probes, health.Probe{ID: "realtime", Check: hub.Probe})
				}
			}
			if s.Health.ConfiguredStorage && services.Storage != nil {
				for _, id := range services.Storage.Disks() {
					disk, err := services.Storage.Disk(id)
					if err != nil {
						return nil, err
					}
					probe, err := checks.Disk(health.ProbeID("storage."+string(id)), disk, checks.DiskProbeKey())
					if err != nil {
						return nil, err
					}
					probes = append(probes, probe)
				}
			}
			if s.Health.ConfiguredMail && services.Mailers != nil {
				for _, name := range services.Mailers.Names() {
					mailer, err := services.Mailers.Mailer(name)
					if err != nil {
						return nil, err
					}
					probe, err := checks.Mailer(health.ProbeID("mail."+string(name)), mailer)
					if err != nil {
						return nil, err
					}
					probes = append(probes, probe)
				}
			}
			return probes, nil
		}))
	}
	if s.Outbox.Enabled {
		return registerOutbox(builder, s.Outbox, source)
	}
	return nil
}
func (s Services) ExtensionStore() (*extensions.Store, error)     { return Resolve(s, ExtensionKey) }
func (s Services) Settings() (*settings.Manager, error)           { return Resolve(s, SettingsKey) }
func (s Services) Metadata() (*metadata.Manager, error)           { return Resolve(s, MetadataKey) }
func (s Services) Translations() (*translations.Manager, error)   { return Resolve(s, TranslationKey) }
func (s Services) Notifications() (*notifications.Manager, error) { return Resolve(s, NotificationKey) }
func (s Services) Attachments() (*attachments.Manager, error)     { return Resolve(s, AttachmentKey) }
func (s Services) Reports() (*datatable.Manager, error)           { return Resolve(s, ReportKey) }
func (s Services) Locales() (*i18n.Catalog, error)                { return Resolve(s, LocaleKey) }
func (s Services) Health() (*health.Registry, error)              { return Resolve(s, HealthKey) }
func (s Services) Audit() (*audit.Recorder, error)                { return Resolve(s, AuditKey) }

// ModelExtensions returns the managers bound model extension slots use,
// resolving only enabled features. Bind it once in a constructor with
// models.<Model>Extensions().From(runtime).
func (s Services) ModelExtensions() (slots.Runtime, error) {
	if !s.features.Extensions.Enabled {
		return slots.Runtime{}, fault.New(fault.Missing, "model extensions require features.extensions")
	}
	var runtime slots.Runtime
	var err error
	if runtime.Store, err = s.ExtensionStore(); err != nil {
		return slots.Runtime{}, err
	}
	if runtime.Metadata, err = s.Metadata(); err != nil {
		return slots.Runtime{}, err
	}
	if s.features.Locales.Enabled {
		if runtime.Translations, err = s.Translations(); err != nil {
			return slots.Runtime{}, err
		}
	}
	if s.features.Attachments.Enabled {
		if runtime.Attachments, err = s.Attachments(); err != nil {
			return slots.Runtime{}, err
		}
	}
	return runtime, nil
}

// validateModelExtensions names the owner whose slots need a disabled feature
// before any resource is acquired.
func validateModelExtensions(s FeatureSettings, models []slots.Declaration) error {
	for _, declaration := range models {
		parts, owner := declaration.Parts(), string(declaration.Owner().Name())
		switch {
		case !s.Extensions.Enabled:
			return fault.New(fault.Invalid, "model extension owner "+owner+" requires features.extensions")
		case len(parts.Translations) > 0 && !s.Locales.Enabled:
			return fault.New(fault.Invalid, "model extension owner "+owner+" declares translated slots, which require features.locales")
		case len(parts.Attachments) > 0 && !s.Attachments.Enabled:
			return fault.New(fault.Invalid, "model extension owner "+owner+" declares attachment slots, which require features.attachments")
		}
	}
	return nil
}
