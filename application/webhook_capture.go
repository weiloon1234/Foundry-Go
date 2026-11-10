package application

import (
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/webhook/capture"
)

// WebhookCaptureProvider owns the capture store after its database boots.
const WebhookCaptureProvider foundation.ProviderID = "foundry.application.webhook-capture"

// WebhookCaptureKey identifies the capture store of an application that
// declares webhook capture.
var WebhookCaptureKey = foundation.NewKey[*capture.Store](string(WebhookCaptureProvider))

// WebhookCaptureSettings are deployment values for declared webhook capture
// (features.webhook_capture). Builder.CaptureWebhooks owns the prefixes and
// full-body opt-ins; without a declaration these settings have no effect.
// Enabled false stops capturing new requests while the store still serves
// the viewer and prunes. Config.ForceRedacted stores every prefix redacted.
// Empty Database selects the default connection; empty Schema selects its
// primary schema, or public for an unscoped connection.
type WebhookCaptureSettings struct {
	Enabled  bool
	Database database.ConnectionName
	Schema   string
	Config   capture.Config
	declared bool `config:"-"`
}

// DefaultWebhookCaptureSettings enables declared capture with
// capture.DefaultConfig on the default connection.
func DefaultWebhookCaptureSettings() WebhookCaptureSettings {
	return WebhookCaptureSettings{Enabled: true, Config: capture.DefaultConfig()}
}

// CaptureWebhooks declares path prefixes whose requests the HTTP kernel
// records, best effort, for the developer viewer (capture.Routes). Each
// declaration is snapshotted now. Build fails when HTTP is disabled, for the
// root prefix, duplicate prefixes, more than 32 prefixes in total, or an
// authenticated route under a declared prefix. The most specific prefix
// selects the body mode.
//
// Example: builder.CaptureWebhooks(http.CapturePrefix(webhooks))
func (b *Builder) CaptureWebhooks(declarations ...http.CaptureDeclaration) *Builder {
	b.mutate(func() {
		if len(declarations) > http.MaxCapturePrefixes-len(b.state.captures) {
			b.state.err = safeerror.NewFault(fault.Invalid, "too many webhook capture prefixes")
			return
		}
		b.state.captures = append(b.state.captures, declarations...)
	})
	return b
}

// WebhookCaptures resolves the capture store, or returns a missing fault
// when the application declares no capture prefix. The store also resolves
// while capture is disabled at runtime, so the viewer keeps working.
func (s Services) WebhookCaptures() (*capture.Store, error) { return Resolve(s, WebhookCaptureKey) }

// WebhookCaptureCommand declares the `webhook-captures prune` operator
// command. It counts by default and removes with --apply. It also works after
// the declaration is removed or capture is disabled, using the configured
// features.webhook_capture database, schema and retention.
func WebhookCaptureCommand() (cli.Declaration, error) {
	return capture.Command(func(r foundation.Resolver) (*capture.Store, error) {
		services, err := FromResolver(r)
		if err != nil {
			return nil, err
		}
		settings := services.features.WebhookCapture
		if settings.declared {
			return foundation.Resolve(r, WebhookCaptureKey)
		}
		if services.Databases == nil {
			return nil, safeerror.NewFault(fault.Missing, "webhook capture database is not configured")
		}
		db, err := services.Databases.Connection(settings.Database)
		if err != nil {
			return nil, err
		}
		return capture.New(nil, capture.Dependencies{DB: db, Schema: settings.Schema, Clock: services.clock, Logger: services.Logger}, settings.Config)
	})
}

// prepareWebhookCapture builds the static recorder and its server option
// without I/O. Disabled capture has neither; the store still registers.
func prepareWebhookCapture(settings WebhookCaptureSettings, declarations []http.CaptureDeclaration) (*httpcapture.Recorder, http.ServerOption, error) {
	if !settings.declared || !settings.Enabled {
		return nil, nil, nil
	}
	recorder, err := capture.NewRecorder(settings.Config)
	if err != nil {
		return nil, nil, err
	}
	return recorder, http.WithWebhookCapture(recorder, declarations...), nil
}

func registerWebhookCapture(builder *foundation.Builder, settings WebhookCaptureSettings, recorder *httpcapture.Recorder) {
	if !settings.declared {
		return
	}
	requires := []foundation.ProviderID{Provider, infrastructure.DatabaseProvider(settings.Database)}
	builder.Register(capture.Module(WebhookCaptureProvider, WebhookCaptureKey, requires, func(r foundation.Resolver) (*capture.Store, error) {
		services, err := FromResolver(r)
		if err != nil {
			return nil, err
		}
		db, err := foundation.Resolve(r, infrastructure.DatabaseKey(settings.Database))
		if err != nil {
			return nil, err
		}
		return capture.New(recorder, capture.Dependencies{DB: db, Schema: settings.Schema, Clock: services.clock, Logger: services.Logger}, settings.Config)
	}))
}
