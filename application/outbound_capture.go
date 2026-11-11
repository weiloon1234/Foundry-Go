package application

import (
	"context"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	outboundcapture "github.com/weiloon1234/Foundry-Go/httpclient/capture"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/internal/capturelog"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	"github.com/weiloon1234/Foundry-Go/internal/outboundcapturestore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// OutboundCaptureProvider owns the outbound capture store after its database boots.
const OutboundCaptureProvider foundation.ProviderID = "foundry.application.outbound-capture"

// OutboundCaptureKey identifies the outbound capture store of an application
// that configures a database.
var OutboundCaptureKey = foundation.NewKey[*outboundcapture.Store](string(OutboundCaptureProvider))

// OutboundCaptureSettings are deployment values for outbound HTTP capture
// (features.outbound_capture). Capture is on whenever the application
// configures a database: every client built by httpclient.Module, including
// settings-named clients, is captured. Settings only reduce capture: Enabled
// false stops capturing while the store still serves the viewer and prunes,
// Config.Exclude skips matching calls, Config.ForceRedacted overrides
// Builder.FullOutboundCapture and Config.MaskNames masks more names. Empty
// Database selects the default connection; empty Schema selects its primary
// schema, or public for an unscoped connection.
type OutboundCaptureSettings struct {
	Enabled  bool
	Database database.ConnectionName
	Schema   string
	Config   outboundcapture.Config
	present  bool `config:"-"`
}

// DefaultOutboundCaptureSettings enables capture with
// outboundcapture.DefaultConfig on the default connection.
func DefaultOutboundCaptureSettings() OutboundCaptureSettings {
	return OutboundCaptureSettings{Enabled: true, Config: outboundcapture.DefaultConfig()}
}

// FullOutboundCapture stores the complete request and response bodies of
// these clients exactly, within config.body_bytes. Credential headers and
// query values stay masked, and config.force_redacted turns it off. Build
// fails when a name matches no client the application constructs.
//
// Example: builder.FullOutboundCapture("smartpay")
func (b *Builder) FullOutboundCapture(clients ...httpclient.Name) *Builder {
	b.mutate(func() {
		if len(clients) > 64-len(b.state.fullOutbound) {
			b.state.err = safeerror.NewFault(fault.Invalid, "too many full outbound capture declarations")
			return
		}
		b.state.fullOutbound = append(b.state.fullOutbound, clients...)
	})
	return b
}

// OutboundCaptures resolves the outbound capture store, or returns a missing
// fault when the application configures no database. The store also resolves
// while capture is disabled, so the viewer keeps working.
func (s Services) OutboundCaptures() (*outboundcapture.Store, error) {
	return Resolve(s, OutboundCaptureKey)
}

// OutboundCaptureCommand declares the `outbound-captures prune` operator
// command. It counts by default and removes with --apply. It also works while
// capture is disabled, using the configured features.outbound_capture
// database, schema, retention and per-client row cap.
func OutboundCaptureCommand() (cli.Declaration, error) {
	return capturelog.Command("outbound-captures", "Count or prune stored outbound HTTP captures in bounded batches", func(r foundation.Resolver) (capturelog.Pruner, error) {
		services, err := FromResolver(r)
		if err != nil {
			return nil, err
		}
		settings := services.features.OutboundCapture
		if !settings.present || services.Databases == nil {
			return nil, safeerror.NewFault(fault.Missing, "outbound capture database is not configured")
		}
		db, err := services.Databases.Connection(settings.Database)
		if err != nil {
			return nil, err
		}
		return capturelog.New[httpcapture.Exchange](outboundcapturestore.Table(), nil, nil, capturelog.Dependencies{DB: db, Schema: settings.Schema, Clock: services.clock, Logger: services.Logger}, capturelog.Limits{Retention: settings.Config.Retention, MaxRows: settings.Config.MaxRows})
	})
}

// prepareOutboundCapture builds the static recorder without I/O. Without a
// database or while disabled there is none, and every client is uncaptured.
func prepareOutboundCapture(settings OutboundCaptureSettings, full []httpclient.Name) (*httpcapture.Outbound, error) {
	if !settings.present || !settings.Enabled {
		return nil, nil
	}
	return outboundcapture.NewRecorder(settings.Config, full)
}

// registerOutboundCapture contributes the recorder every httpclient.Module
// reads and registers the store. Build registers it before the
// infrastructure plan: providers boot depth-first in registration order and
// close in reverse, so the store boots right after its database and closes
// after every client has drained. The store closes over the Build-time clock
// and logger instead of requiring the application provider.
func registerOutboundCapture(builder *foundation.Builder, settings OutboundCaptureSettings, recorder *httpcapture.Outbound, source clock.Clock, logger *slog.Logger) {
	if !settings.present {
		return
	}
	if recorder != nil {
		builder.Register(foundation.Module{Name: OutboundCaptureProvider + ".recorder", OnRegister: func(r *foundation.Registrar) error {
			return foundation.Contribute(r, httpcapture.OutboundSinks, "recorder", func(foundation.Resolver) (*httpcapture.Outbound, error) { return recorder, nil })
		}})
	}
	requires := []foundation.ProviderID{infrastructure.DatabaseProvider(settings.Database)}
	builder.Register(outboundcapture.Module(OutboundCaptureProvider, OutboundCaptureKey, requires, func(r foundation.Resolver) (*outboundcapture.Store, error) {
		db, err := foundation.Resolve(r, infrastructure.DatabaseKey(settings.Database))
		if err != nil {
			return nil, err
		}
		return outboundcapture.New(recorder, outboundcapture.Dependencies{DB: db, Schema: settings.Schema, Clock: source, Logger: logger}, settings.Config)
	}))
}

// checkOutboundCapture runs after every client was constructed. A full-body
// declaration for a client the application never builds fails Build; a
// client-only exclusion that matched nothing is logged once, because one
// settings file serves environments whose conditional clients differ.
func checkOutboundCapture(ctx context.Context, recorder *httpcapture.Outbound, full []httpclient.Name, exclusions []outboundcapture.Exclusion, logger *slog.Logger) error {
	if recorder == nil {
		return nil
	}
	for _, name := range full {
		if !recorder.Claimed(string(name)) {
			return safeerror.NewFault(fault.Missing, "full outbound capture names a client the application does not construct")
		}
	}
	for _, exclusion := range exclusions {
		if exclusion.Client != "" && exclusion.Host == "" && exclusion.Method == "" && exclusion.PathPrefix == "" && !recorder.Claimed(string(exclusion.Client)) {
			logger.WarnContext(ctx, "outbound capture exclusion names a client the application does not construct", "client", string(exclusion.Client))
		}
	}
	return nil
}
