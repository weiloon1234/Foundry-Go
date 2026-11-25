package consumer_test

import (
	"context"

	"errors"
	"foundry.test/consumer/tooling"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/diagnostics"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/telemetry"
)

type telemetryLifecycle interface {
	Run(context.Context) error
	Close(context.Context) error
	Done() <-chan struct{}
	Reader() *telemetry.Reader
	RecordBrowser(context.Context, telemetry.BrowserError) error
	Prune(context.Context) (telemetry.PruneResult, error)
	AcknowledgeIn(context.Context, *database.Tx, telemetry.AlertKey) (telemetry.Acknowledgement, error)
}

type telemetryConsoleReads interface {
	Overview(context.Context, telemetry.OverviewQuery) (telemetry.Overview, error)
	Traffic(context.Context, telemetry.TrafficQuery) (telemetry.Traffic, error)
	Issue(context.Context, telemetry.Fingerprint) (telemetry.Issue, error)
	Issues(context.Context, telemetry.IssueQuery) (orm.PageResult[telemetry.Issue], error)
	Events(context.Context, telemetry.EventQuery) (orm.PageResult[telemetry.Event], error)
	Alerts(context.Context, telemetry.AlertQuery) (orm.PageResult[telemetry.Alert], error)
}

var (
	_ telemetryLifecycle                                                                                                                                                                       = (*telemetry.Engine)(nil)
	_ telemetryConsoleReads                                                                                                                                                                    = (*telemetry.Reader)(nil)
	_ func(*telemetry.Collector, telemetry.Dependencies, telemetry.Config) (*telemetry.Engine, error)                                                                                          = telemetry.New
	_ func(foundation.ProviderID, foundation.Key[*telemetry.Engine], []foundation.KernelKind, []foundation.ProviderID, func(foundation.Resolver) (*telemetry.Engine, error)) foundation.Module = telemetry.KernelModule
)

// Like the database transaction example, this is compile-checked public assembly.
// The caller registers collector.Report when constructing the borrowed recorder.
func Example_telemetryDependencies() {
	prepare := func(collector *telemetry.Collector, db *database.DB, recorder *observability.Recorder, readiness *diagnostics.Runtime, source clock.Clock, logger *slog.Logger, config telemetry.Config) (*telemetry.Engine, error) {
		return telemetry.New(collector, telemetry.Dependencies{
			DB: db, Schema: "operations", Recorder: recorder,
			Readiness: readiness, Clock: source, Logger: logger,
		}, config)
	}
	_ = prepare
}

// Actor policy and attribution execute in the same transaction as acknowledgement.
func Example_telemetryAcknowledgement() {
	acknowledge := tooling.AcknowledgeAlert
	_ = acknowledge
}

func TestConsumerTelemetryDTOsRetainConcreteIdentityOwners(t *testing.T) {
	at := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	runID, err := model.NewIDAt[telemetry.Run](at)
	if err != nil {
		t.Fatal(err)
	}
	eventID, err := model.NewIDAt[telemetry.Event](at)
	if err != nil {
		t.Fatal(err)
	}
	run := telemetry.Run{ID: runID}
	event := telemetry.Event{ID: eventID}
	alert := telemetry.Alert{RunID: run.ID, Episode: 2, State: telemetry.AlertStateOpen}
	if alert.RunID != runID || event.ID != eventID {
		t.Fatal("public snapshots lost their concrete identity")
	}
	attribution := tooling.TelemetryActorAttribution{Episode: 1}
	if attribution.Episode == alert.Episode {
		t.Fatal("old actor attribution matched the reopened episode")
	}
	if err := (telemetry.Window{Minutes: 15}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestConsumerDisabledTelemetryDoesNotRequireDatabaseOrReadiness(t *testing.T) {
	settings := application.DefaultSettings()
	settings.HTTP.Enabled = false
	settings.Features.Telemetry = application.TelemetrySettings{Database: "unconfigured", Schema: "unused-schema"}
	app, err := application.New(settings, application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))).
		TelemetryReadiness(foundation.NewKey[*diagnostics.Runtime]("consumer.optional-readiness")).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	if _, err := app.Resources().Telemetry(); !errors.Is(err, fault.Missing) {
		t.Fatalf("disabled telemetry service = %v", err)
	}
	for _, target := range app.Migrations() {
		for _, definition := range target.Definitions {
			if definition.Key.Origin == "foundry.telemetry" {
				t.Fatal("disabled telemetry added a migration target")
			}
		}
	}
}
