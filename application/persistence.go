package application

import (
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/audit"
	mfapg "github.com/weiloon1234/Foundry-Go/auth/mfa/postgres"
	sessionpg "github.com/weiloon1234/Foundry-Go/auth/session/postgres"
	tokenpg "github.com/weiloon1234/Foundry-Go/auth/token/postgres"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	outboundcapture "github.com/weiloon1234/Foundry-Go/httpclient/capture"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/jobs/archive"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/notifications"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/settings"
	"github.com/weiloon1234/Foundry-Go/telemetry"
	"github.com/weiloon1234/Foundry-Go/translations"
	"github.com/weiloon1234/Foundry-Go/webhook/capture"
)

type persistenceTarget struct {
	enabled     bool
	connection  *database.ConnectionName
	schema      *string
	definitions func() []migrate.Definition
}

func featurePersistenceTargets(s *FeatureSettings) []persistenceTarget {
	var targets []persistenceTarget
	add := func(enabled bool, connection *database.ConnectionName, schema *string, definitions func() []migrate.Definition) {
		targets = append(targets, persistenceTarget{enabled, connection, schema, definitions})
	}
	add(s.Auth.Sessions.Enabled, &s.Auth.Sessions.Database, &s.Auth.Sessions.Schema, sessionpg.Migrations)
	add(s.Auth.Tokens.Enabled, &s.Auth.Tokens.Database, &s.Auth.Tokens.Schema, tokenpg.Migrations)
	add(s.Auth.MFA.Enabled, &s.Auth.MFA.Database, &s.Auth.MFA.Schema, mfapg.Migrations)
	add(s.Idempotency.Enabled, &s.Idempotency.Database, &s.Idempotency.Config.Schema, idempotency.Migrations)
	add(s.Outbox.Enabled, &s.Outbox.Database, &s.Outbox.Schema, outbox.Migrations)
	add(s.Audit.Enabled, &s.Audit.Database, &s.Audit.Schema, audit.Migrations)
	add(s.Telemetry.Enabled, &s.Telemetry.Database, &s.Telemetry.Schema, telemetry.Migrations)
	add(s.Notifications.Enabled, &s.Notifications.Database, &s.Notifications.Schema, notifications.Migrations)
	add(s.WebhookCapture.declared, &s.WebhookCapture.Database, &s.WebhookCapture.Schema, capture.Migrations)
	add(s.OutboundCapture.present, &s.OutboundCapture.Database, &s.OutboundCapture.Schema, outboundcapture.Migrations)
	add(s.Extensions.Enabled, &s.Extensions.Database, &s.Extensions.Schema, settings.Migrations)
	add(s.Extensions.Enabled, &s.Extensions.Database, &s.Extensions.Schema, metadata.Migrations)
	add(s.Extensions.Enabled && s.Locales.Enabled, &s.Extensions.Database, &s.Extensions.Schema, translations.Migrations)
	add(s.Extensions.Enabled && s.Attachments.Enabled, &s.Extensions.Database, &s.Extensions.Schema, attachments.Migrations)
	add(s.Reports.Enabled, &s.Reports.Database, &s.Reports.Config.Schema, nil)
	return targets
}

func persistenceTargets(s *Settings) []persistenceTarget {
	targets := featurePersistenceTargets(&s.Features)
	return append(targets, persistenceTarget{s.Worker.Archive.Enabled, &s.Worker.Archive.Database, &s.Worker.Archive.Schema, archive.Migrations})
}
