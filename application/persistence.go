package application

import (
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/audit"
	sessionpg "github.com/weiloon1234/Foundry-Go/auth/session/postgres"
	tokenpg "github.com/weiloon1234/Foundry-Go/auth/token/postgres"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/notifications"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/settings"
	"github.com/weiloon1234/Foundry-Go/translations"
)

type persistenceTarget struct {
	connection  *database.ConnectionName
	schema      *string
	definitions func() []migrate.Definition
}

func persistenceTargets(s *FeatureSettings) []persistenceTarget {
	var targets []persistenceTarget
	add := func(enabled bool, connection *database.ConnectionName, schema *string, definitions func() []migrate.Definition) {
		if enabled {
			targets = append(targets, persistenceTarget{connection, schema, definitions})
		}
	}
	add(s.Auth.Sessions.Enabled, &s.Auth.Sessions.Database, &s.Auth.Sessions.Schema, sessionpg.Migrations)
	add(s.Auth.Tokens.Enabled, &s.Auth.Tokens.Database, &s.Auth.Tokens.Schema, tokenpg.Migrations)
	add(s.Idempotency.Enabled, &s.Idempotency.Database, &s.Idempotency.Config.Schema, idempotency.Migrations)
	add(s.Outbox.Enabled, &s.Outbox.Database, &s.Outbox.Schema, outbox.Migrations)
	add(s.Audit.Enabled, &s.Audit.Database, &s.Audit.Schema, audit.Migrations)
	add(s.Notifications.Enabled, &s.Notifications.Database, &s.Notifications.Schema, notifications.Migrations)
	if s.Extensions.Enabled {
		add(true, &s.Extensions.Database, &s.Extensions.Schema, func() []migrate.Definition {
			definitions := append(settings.Migrations(), metadata.Migrations()...)
			if s.Locales.Enabled {
				definitions = append(definitions, translations.Migrations()...)
			}
			if s.Attachments.Enabled {
				definitions = append(definitions, attachments.Migrations()...)
			}
			return definitions
		})
	}
	add(s.Reports.Enabled, &s.Reports.Database, &s.Reports.Config.Schema, nil)
	return targets
}
