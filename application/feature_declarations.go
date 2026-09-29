package application

import (
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/health"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/notifications"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
	"github.com/weiloon1234/Foundry-Go/settings"
	"github.com/weiloon1234/Foundry-Go/translations"
)

// FeatureDeclarations retains the framework's typed registration envelopes.
// This is an assembly boundary, never a dynamic runtime model/service container.
type FeatureDeclarations struct {
	Owners        []extensions.Declaration
	Settings      []settings.Registration
	Metadata      []metadata.Registration
	Translations  []translations.Registration
	Attachments   []attachments.Registration
	Notifications []notifications.Registration
	Reports       []datatable.Registration
	Messages      []i18n.MessageDefinition
	Catalog       map[i18n.LocaleID]map[i18n.MessageKey]i18n.Template
	Outbox        []publisher.Route
	Readiness     []health.Probe
	// Pruning joins application stores to the maintenance schedule
	// (features.maintenance); it is ignored while that schedule is disabled.
	Pruning []Pruning
}
type Features func(Services) (FeatureDeclarations, error)

func (b *Builder) Features(construct Features) *Builder {
	b.mutate(func() {
		if construct == nil {
			b.state.err = fault.New(fault.Invalid, "nil feature declarations")
			return
		}
		b.state.features = append(b.state.features, construct)
	})
	return b
}

const FeatureDeclarationsProvider foundation.ProviderID = "foundry.application.feature-declarations"

var featureDeclarationsKey = foundation.NewKey[FeatureDeclarations](string(FeatureDeclarationsProvider))

func mergeFeatures(r foundation.Resolver, s FeatureSettings, constructors []Features) (FeatureDeclarations, error) {
	var result FeatureDeclarations
	services, err := FromResolver(r)
	if err != nil {
		return result, err
	}
	for _, construct := range constructors {
		d, err := construct(services)
		if err != nil {
			return result, err
		}
		result.Owners = append(result.Owners, d.Owners...)
		result.Settings = append(result.Settings, d.Settings...)
		result.Metadata = append(result.Metadata, d.Metadata...)
		result.Translations = append(result.Translations, d.Translations...)
		result.Attachments = append(result.Attachments, d.Attachments...)
		result.Notifications = append(result.Notifications, d.Notifications...)
		result.Reports = append(result.Reports, d.Reports...)
		result.Messages = append(result.Messages, d.Messages...)
		result.Outbox = append(result.Outbox, d.Outbox...)
		result.Readiness = append(result.Readiness, d.Readiness...)
		result.Pruning = append(result.Pruning, d.Pruning...)
		if result.Catalog == nil {
			result.Catalog = make(map[i18n.LocaleID]map[i18n.MessageKey]i18n.Template)
		}
		for locale, messages := range d.Catalog {
			if result.Catalog[locale] == nil {
				result.Catalog[locale] = make(map[i18n.MessageKey]i18n.Template)
			}
			for key, template := range messages {
				if _, exists := result.Catalog[locale][key]; exists {
					return result, fault.New(fault.Duplicate, "locale message was declared twice")
				}
				result.Catalog[locale][key] = template
			}
		}
	}
	for _, condition := range []bool{
		!s.Extensions.Enabled && (len(result.Owners)+len(result.Settings)+len(result.Metadata)+len(result.Translations) > 0),
		!s.Locales.Enabled && (len(result.Messages)+len(result.Catalog)+len(result.Translations) > 0),
		!s.Attachments.Enabled && len(result.Attachments) > 0,
		!s.Notifications.Enabled && len(result.Notifications) > 0,
		!s.Reports.Enabled && len(result.Reports) > 0,
		!s.Outbox.Enabled && len(result.Outbox) > 0,
		!s.Health.Enabled && len(result.Readiness) > 0,
	} {
		if condition {
			return result, fault.New(fault.Invalid, "feature declarations require the corresponding feature to be enabled")
		}
	}
	return result, nil
}
