package eventqueries_test

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"foundry.test/consumer/eventqueries"
	"foundry.test/consumer/observerqueries"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/outbox"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	redistest "github.com/weiloon1234/Foundry-Go/testkit/redis"
)

func TestConfiguredEventOutboxThroughRedisWorker(t *testing.T) {
	migrationDB := pgtest.Open(t)
	schema := pgtest.Namespace(t, migrationDB)
	s := application.DefaultSettings()
	s.HTTP.Enabled = false
	s.Services.Namespace.Application = "b26-" + rand.Text()
	dbSettings := infrastructure.DefaultConnectionSettings()
	dbSettings.Primary = infrastructure.PostgreSQLSettingsFromConfig(pgtest.Config(t))
	dbSettings.Primary.Schema = schema
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": dbSettings}
	s.Services.Redis.Connections = infrastructure.RedisConnections{"default": infrastructure.RedisSettingsFromConfig(redistest.Config(t))}
	// The delivery connection is deliberately not the default connection.
	s.Services.Jobs.Connections = infrastructure.JobConnections{"default": infrastructure.DefaultJobConnectionSettings()}
	s.Features.Outbox.Schema = schema
	s.Features.Outbox.PollInterval, s.Features.Outbox.MaxPollInterval = time.Millisecond, 5*time.Millisecond
	s.Features.Outbox.Kernels = []foundation.KernelKind{foundation.Worker}
	s.Worker.Config.PollInterval = time.Millisecond
	s.Worker.Config.Queues = []jobs.Subscription{{Queue: "deliveries", Weight: 1}, {Queue: "listeners", Weight: 1}}
	settingsSchema, err := application.SettingsConfigSchema()
	if err != nil {
		t.Fatal(err)
	}
	layer, err := toml.Decode(strings.NewReader(`
[services.jobs.connections.default]
driver = 'memory'
[services.jobs.connections.publications]
driver = 'redis'
default_queue = 'deliveries'
[worker]
enabled = true
connection = 'publications'
[features.events]
enabled = true
queued_listeners = true
listener_connection = 'publications'
listener_queue = 'listeners'
[features.outbox]
enabled = true
[features.outbox.events]
'consumer.publications' = 'publications'
'consumer.secondary' = 'publications'
`), settingsSchema, toml.Options{Name: "queued-event-outbox"})
	if err != nil {
		t.Fatal(err)
	}
	s, _, err = settingsSchema.Load(s, config.Inputs[application.Settings]{Files: []config.Values{layer}})
	if err != nil {
		t.Fatal(err)
	}
	secondary := foundation.NewKey[*events.Outbox]("consumer.secondary")
	handled := events.Define[eventqueries.RecordCreated]("records.handled", 1)
	retried := events.Define[eventqueries.RecordCreated]("records.retried", 1)
	var mu sync.Mutex
	inline, queued, attempts := 0, 0, 0
	var retryIDs []outbox.ID[eventqueries.RecordCreated]
	var handledID model.ID[observerqueries.Plain]
	app, err := eventqueries.ConfigurePublications(application.New(s, application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))).Events(
		application.EventOutbox(secondary, "consumer.secondary"),
		application.Listen(handled, "inline", func(application.Services) (events.Handler[eventqueries.RecordCreated], error) {
			return func(ctx context.Context, p eventqueries.RecordCreated) error {
				mu.Lock()
				defer mu.Unlock()
				if _, ok := handled.OutboxID(ctx); !ok {
					return errors.New("inline listener lost outbox identity")
				}
				handledID = p.ID
				inline++
				return nil
			}, nil
		}),
		application.ListenQueued(handled, "queued", func(application.Services) (events.Handler[eventqueries.RecordCreated], error) {
			return func(_ context.Context, p eventqueries.RecordCreated) error {
				mu.Lock()
				defer mu.Unlock()
				if p.ID != handledID {
					return errors.New("queued listener lost typed payload")
				}
				queued++
				return nil
			}, nil
		}),
		application.Listen(retried, "retry", func(application.Services) (events.Handler[eventqueries.RecordCreated], error) {
			return func(ctx context.Context, _ eventqueries.RecordCreated) error {
				mu.Lock()
				defer mu.Unlock()
				id, ok := retried.OutboxID(ctx)
				if !ok {
					return errors.New("retry lost outbox identity")
				}
				retryIDs = append(retryIDs, id)
				attempts++
				if attempts == 1 {
					return errors.New("transient listener failure")
				}
				return nil
			}, nil
		}),
	).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	started := false
	t.Cleanup(func() {
		cancel()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := app.Shutdown(stopCtx); err != nil {
			t.Error(err)
		}
		if started {
			select {
			case err := <-done:
				if err != nil && !errors.Is(err, context.Canceled) {
					t.Error(err)
				}
			case <-stopCtx.Done():
				t.Error("configured worker did not drain")
			}
		}
	})
	if err := migrationDB.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`", pg_temp`); err != nil {
			return err
		}
		if _, err := tx.Exec(t.Context(), `CREATE TABLE observed_plain(id uuid PRIMARY KEY,name text NOT NULL)`); err != nil {
			return err
		}
		for _, target := range app.Migrations() {
			for _, definition := range target.Definitions {
				for _, statement := range definition.SQL {
					if _, err := tx.Exec(t.Context(), statement); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	started = true
	go func() { done <- app.Run(ctx, foundation.Worker) }()
	db, err := app.Resources().Database()
	if err != nil {
		t.Fatal(err)
	}
	producer, err := application.Resolve(app.Resources(), eventqueries.Publications)
	if err != nil {
		t.Fatal(err)
	}
	otherProducer, err := application.Resolve(app.Resources(), secondary)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := app.Resources().Jobs.Connection("publications")
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := connection.Dispatcher()
	publish := func(topic events.Topic[eventqueries.RecordCreated], producer *events.Outbox, rollback bool) outbox.ID[eventqueries.RecordCreated] {
		t.Helper()
		var id outbox.ID[eventqueries.RecordCreated]
		rollbackErr := errors.New("outer rollback")
		err := db.Transaction(t.Context(), func(tx *database.Tx) error {
			item, err := observerqueries.QueryObservedPlain().Create(t.Context(), tx, observerqueries.PlainDraft{}.SetName("published"))
			if err != nil {
				return err
			}
			id, err = topic.Enqueue(t.Context(), tx, producer, eventqueries.RecordCreated{ID: item.ID})
			if err != nil {
				return err
			}
			record, err := dispatcher.Inspect(t.Context(), "deliveries", model.IDFromBytes[jobs.Execution](id.Bytes()))
			if err != nil {
				return err
			}
			if record.IsSet() {
				return errors.New("event job was published before outer commit")
			}
			if rollback {
				return rollbackErr
			}
			return nil
		})
		if rollback && !errors.Is(err, rollbackErr) || !rollback && err != nil {
			t.Fatal(err)
		}
		return id
	}
	waitPublished := func(id outbox.ID[eventqueries.RecordCreated], expectedAttempts uint32) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			var state outbox.PublicationState
			if err := database.ScanOne(t.Context(), db, "SELECT publish_state FROM foundry_outbox WHERE id = $1", []any{id.String()}, &state); err != nil {
				t.Fatal(err)
			}
			result, err := dispatcher.Inspect(t.Context(), "deliveries", model.IDFromBytes[jobs.Execution](id.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			if record, ok := result.Get(); ok && state == outbox.Published && record.State == jobs.Succeeded {
				if record.Envelope.Name() != "foundry.events.deliver" || record.Attempts != expectedAttempts {
					t.Fatal("unexpected framework delivery job", record.Summary())
				}
				if record.Envelope.Policy().Attempts != jobs.DefaultPolicy("deliveries").Attempts {
					t.Fatal("delivery policy changed")
				}
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("event row/job did not reach published/succeeded")
	}
	rolledBack := publish(eventqueries.Created, producer, true)
	zero := publish(eventqueries.Created, producer, false)
	waitPublished(zero, 1) // No placeholder listener: the framework job succeeds.
	mixed := publish(handled, otherProducer, false)
	waitPublished(mixed, 1)
	deadline := time.Now().Add(5 * time.Second)
	for {
		page, err := dispatcher.List(t.Context(), "listeners", jobs.ListOptions{State: jobs.Succeeded, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Records) == 1 {
			r := page.Records[0]
			if r.Envelope.Name() != "foundry.events.listener" || r.Attempts != 1 || r.Envelope.ID().Bytes() == mixed.Bytes() {
				t.Fatal("queued listener did not run as its own job")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("queued listener did not complete")
		}
		time.Sleep(5 * time.Millisecond)
	}
	retry := publish(retried, producer, false)
	waitPublished(retry, 2)
	mu.Lock()
	if inline != 1 || queued != 1 || attempts != 2 || len(retryIDs) != 2 || retryIDs[0] != retry || retryIDs[1] != retry {
		t.Error("listener counts or retry identity changed")
	}
	mu.Unlock()
	var rows int
	if err := database.ScanOne(t.Context(), db, "SELECT count(*) FROM foundry_outbox WHERE id = $1", []any{rolledBack.String()}, &rows); err != nil || rows != 0 {
		t.Fatal("rollback retained event row", err)
	}
	result, err := dispatcher.Inspect(t.Context(), "deliveries", model.IDFromBytes[jobs.Execution](rolledBack.Bytes()))
	if err != nil || result.IsSet() {
		t.Fatal("rollback retained delivery job", err)
	}
	page, err := dispatcher.List(t.Context(), "deliveries", jobs.ListOptions{Limit: 10})
	if err != nil || len(page.Records) != 3 {
		t.Fatal("unexpected event delivery jobs", err)
	}
}
