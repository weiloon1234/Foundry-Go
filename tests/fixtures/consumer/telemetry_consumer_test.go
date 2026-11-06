package consumer_test

import (
	"context"
	"errors"
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

type telemetryConsoleActor struct {
	ID model.ID[telemetryConsoleActor]
}

type telemetryActorAttribution struct {
	Key     telemetry.AlertKey
	Episode telemetry.Episode
	Actor   model.ID[telemetryConsoleActor]
}

type telemetryActorRepository interface {
	LockAuthorized(context.Context, *database.Tx, model.ID[telemetryConsoleActor]) (telemetryConsoleActor, error)
	RecordAcknowledgement(context.Context, *database.Tx, telemetryActorAttribution) error
}

// Actor policy and attribution remain consumer-owned, in the caller's actual
// transaction. Returning from AcknowledgeIn alone does not promise commit.
func Example_telemetryAcknowledgement() {
	acknowledge := func(ctx context.Context, db *database.DB, engine *telemetry.Engine, actors telemetryActorRepository, actorID model.ID[telemetryConsoleActor], key telemetry.AlertKey) error {
		return db.Transaction(ctx, func(tx *database.Tx) error {
			actor, err := actors.LockAuthorized(ctx, tx, actorID)
			if err != nil {
				return err
			}
			ack, err := engine.AcknowledgeIn(ctx, tx, key)
			if err != nil {
				return err
			}
			return actors.RecordAcknowledgement(ctx, tx, telemetryActorAttribution{Key: ack.Key, Episode: ack.Episode, Actor: actor.ID})
		})
	}
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
	attribution := telemetryActorAttribution{Episode: 1}
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
